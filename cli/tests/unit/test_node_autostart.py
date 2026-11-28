import os
import plistlib
from pathlib import Path

from inferyard.node.autostart import (
    WINDOWS_RUN_KEY,
    AutostartManager,
    fallback_script,
    systemd_unit,
    windows_run_command,
)
from inferyard.node.credentials import windows_acl_command


def test_macos_launch_agent_uses_injected_directory(tmp_path):
    state = tmp_path / "state"
    root = tmp_path / "agents"
    calls = []

    def run(command, **kwargs):
        calls.append(command)

    result = AutostartManager(
        state, system="Darwin", runner=run, directories={"launch_agents": root}
    ).install()
    document = plistlib.loads(Path(result["path"]).read_bytes())
    assert result["mode"] == "launch_agent"
    assert document["Label"] == "io.inferyard.agent"
    assert document["RunAtLoad"] is True
    assert document["KeepAlive"] is True
    assert document["ProgramArguments"][-2:] == ["--state-dir", str(state)]
    assert calls[0][0] == "launchctl"
    AutostartManager(
        state, system="Darwin", runner=run, directories={"launch_agents": root}
    ).uninstall()
    assert not Path(result["path"]).exists()


def test_linux_systemd_and_container_fallback(tmp_path, monkeypatch):
    state = tmp_path / "state"
    root = tmp_path / "systemd"
    result = AutostartManager(
        state, system="Linux", runner=lambda *args, **kwargs: None,
        directories={"systemd": root},
    ).install(activate=False)
    text = Path(result["path"]).read_text()
    assert "Restart=on-failure" in text
    assert "WantedBy=default.target" in text
    assert str(state) in systemd_unit(state)

    monkeypatch.setenv("HOME", str(tmp_path / "fake-home"))
    monkeypatch.setenv("XDG_CONFIG_HOME", str(tmp_path / "fake-xdg"))
    monkeypatch.setattr("inferyard.node.autostart.shutil.which", lambda _: "/usr/bin/systemctl")
    fallback = AutostartManager(
        state,
        system="Linux",
        runner=lambda *args, **kwargs: None,
        directories={"fallback": tmp_path / "fallback"},
    )
    result = fallback.install(activate=False)
    assert result["mode"] == "background_script"
    assert not (tmp_path / "fake-xdg" / "systemd").exists()
    script = Path(result["path"])
    assert "nohup" in script.read_text()
    assert "agent.pid" in fallback_script(state)
    if os.name != "nt":
        assert script.stat().st_mode & 0o777 == 0o700


def test_windows_run_and_acl_commands_are_user_scoped(tmp_path):
    command = windows_run_command(tmp_path)
    assert command[:5] == ["reg", "add", WINDOWS_RUN_KEY, "/v", "InferYardAgent"]
    file_acl = windows_acl_command(tmp_path / "client.key", "S-1-5-21-100", is_dir=False)
    assert file_acl[:3] == ["icacls", str(tmp_path / "client.key"), "/inheritance:r"]
    assert "*S-1-5-21-100:F" in file_acl
    assert "*S-1-5-21-100:(OI)(CI)F" not in file_acl
    dir_acl = windows_acl_command(tmp_path, "S-1-5-21-100")
    assert "*S-1-5-21-100:(OI)(CI)F" in dir_acl


def test_windows_install_registers_hkcu_run_and_spawns_agent(tmp_path):
    calls = []
    spawned = []
    test_key = r"HKCU\Software\InferYardTest\Run"

    def run(command, **kwargs):
        calls.append(command)

    manager = AutostartManager(
        tmp_path,
        system="Windows",
        runner=run,
        registry_key=test_key,
        spawner=lambda d: (spawned.append(d) or 4321),
    )
    result = manager.install()
    assert result["mode"] == "registry_run"
    assert result["key"] == test_key
    assert result["pid"] == 4321
    assert spawned == [tmp_path]
    assert calls[0][:5] == ["reg", "add", test_key, "/v", "InferYardAgent"]
    manager.uninstall()
    assert calls[1] == ["reg", "delete", test_key, "/v", "InferYardAgent", "/f"]


def test_linux_headless_systemd_user_failure_falls_back_to_background_script(
    tmp_path, monkeypatch
):
    import subprocess

    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setenv("XDG_CONFIG_HOME", str(tmp_path / "xdg"))
    monkeypatch.setattr("inferyard.node.autostart.shutil.which", lambda _: "/usr/bin/systemctl")
    monkeypatch.setattr("pathlib.Path.is_dir", lambda self: True)
    calls = []

    def runner(cmd, **kwargs):
        calls.append(cmd)
        if cmd[0] == "systemctl":
            raise subprocess.CalledProcessError(1, cmd)

    result = AutostartManager(tmp_path / "state", system="Linux", runner=runner).install()
    assert result["mode"] == "background_script"
    assert not (tmp_path / "xdg" / "systemd" / "user" / "inferyard-agent.service").exists()
