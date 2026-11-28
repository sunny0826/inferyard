import json
import plistlib
from pathlib import Path

from inferyard.node.autostart import (
    AutostartManager,
    fallback_script,
    systemd_unit,
    windows_task_command,
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


def test_linux_systemd_and_container_fallback(tmp_path):
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

    fallback = AutostartManager(
        state, system="Linux", runner=lambda *args, **kwargs: None,
        directories={},
    )
    fallback.directories["fallback"] = tmp_path / "fallback"
    fallback.system = "Linux"
    result = fallback.install(activate=False)
    assert result["mode"] == "background_script"
    script = Path(result["path"])
    assert "nohup" in script.read_text()
    assert "agent.pid" in fallback_script(state)
    assert script.stat().st_mode & 0o777 == 0o700


def test_windows_task_and_acl_commands_are_user_scoped(tmp_path):
    command = windows_task_command(tmp_path)
    assert command[:4] == ["schtasks", "/Create", "/TN", "InferYardAgent"]
    assert "/RL" in command and "LIMITED" in command
    acl = windows_acl_command(tmp_path / "client.key", "S-1-5-21-100")
    assert acl[:3] == ["icacls", str(tmp_path / "client.key"), "/inheritance:r"]
    assert "*S-1-5-21-100:(OI)(CI)F" in acl


def test_windows_install_starts_agent_in_current_session(tmp_path):
    calls = []

    def run(command, **kwargs):
        calls.append(command)

    result = AutostartManager(tmp_path, system="Windows", runner=run).install()
    assert result["mode"] == "scheduled_task"
    assert [call[:2] for call in calls] == [["schtasks", "/Create"], ["schtasks", "/Run"]]
    assert calls[1][2:] == ["/TN", "InferYardAgent"]
