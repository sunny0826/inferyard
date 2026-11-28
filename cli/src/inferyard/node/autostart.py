"""Per-user node agent autostart manifests for Windows, macOS and Linux."""

from __future__ import annotations

import json
import os
import platform
import plistlib
import shlex
import shutil
import subprocess
import sys
from pathlib import Path


def agent_command(state_dir: Path, *, windows: bool = False) -> list[str]:
    executable = sys.executable
    if windows:
        configured = os.environ.get("INFERYARD_PYTHONW")
        pythonw = Path(configured) if configured else Path(executable).with_name("pythonw.exe")
        if pythonw.is_file():
            executable = str(pythonw)
    return [executable, "-m", "inferyard", "agent", "run", "--state-dir", str(state_dir)]


def launch_agent(state_dir: Path) -> dict:
    command = agent_command(state_dir)
    return {
        "Label": "io.inferyard.agent",
        "ProgramArguments": command,
        "RunAtLoad": True,
        "KeepAlive": True,
        "ProcessType": "Background",
        "StandardOutPath": str(state_dir / "agent.log"),
        "StandardErrorPath": str(state_dir / "agent.log"),
    }


def systemd_unit(state_dir: Path) -> str:
    command = shlex.join(agent_command(state_dir))
    return (
        "[Unit]\nDescription=InferYard Node Agent\nAfter=network-online.target\n\n"
        "[Service]\nType=simple\n"
        f"ExecStart={command}\nRestart=on-failure\nRestartSec=5\n"
        f"WorkingDirectory={state_dir}\n\n"
        "[Install]\nWantedBy=default.target\n"
    )


def fallback_script(state_dir: Path) -> str:
    command = shlex.join(agent_command(state_dir))
    log = shlex.quote(str(state_dir / "agent.log"))
    pid = shlex.quote(str(state_dir / "agent.pid"))
    return (
        "#!/bin/sh\nset -eu\n"
        f"if [ -f {pid} ] && kill -0 \"$(cat {pid})\" 2>/dev/null; then exit 0; fi\n"
        f"nohup {command} >> {log} 2>&1 &\n"
        f"echo $! > {pid}\n"
    )


WINDOWS_RUN_KEY = r"HKCU\Software\Microsoft\Windows\CurrentVersion\Run"
WINDOWS_RUN_VALUE = "InferYardAgent"


def _uid() -> int:
    getuid = getattr(os, "getuid", None)
    return getuid() if callable(getuid) else 0


def windows_run_key(override: str | None = None) -> str:
    return override or os.environ.get("INFERYARD_WINDOWS_RUN_KEY") or WINDOWS_RUN_KEY


def windows_run_command(state_dir: Path, *, registry_key: str | None = None) -> list[str]:
    command = subprocess.list2cmdline(agent_command(state_dir, windows=True))
    return [
        "reg",
        "add",
        windows_run_key(registry_key),
        "/v",
        WINDOWS_RUN_VALUE,
        "/t",
        "REG_SZ",
        "/d",
        command,
        "/f",
    ]


def _start_windows_background(state_dir: Path) -> int | None:
    if os.name != "nt" or os.environ.get("INFERYARD_AUTOSTART_NO_SPAWN") == "1":
        return None
    from inferyard.errors import PreflightError
    from inferyard.platforms.identity import process_start_ticks

    pid_path = state_dir / "agent.pid"
    if pid_path.is_file():
        try:
            existing = json.loads(pid_path.read_text(encoding="utf-8"))
            pid, ticks = existing.get("pid"), existing.get("start_ticks")
            if isinstance(pid, int) and isinstance(ticks, int) and process_start_ticks(pid) == ticks:
                return pid
        except (OSError, ValueError, PreflightError):
            pass
    log_path = state_dir / "agent.log"
    with log_path.open("ab") as output:
        process = subprocess.Popen(
            agent_command(state_dir, windows=True),
            cwd=str(state_dir),
            stdin=subprocess.DEVNULL,
            stdout=output,
            stderr=subprocess.STDOUT,
            close_fds=True,
            creationflags=0x00000008 | 0x08000000,
        )
    try:
        ticks = process_start_ticks(process.pid)
    except (OSError, PreflightError):
        ticks = None
    pid_path.write_text(
        json.dumps({"pid": process.pid, "start_ticks": ticks}) + "\n", encoding="utf-8"
    )
    return process.pid


def _stop_windows_background(state_dir: Path) -> None:
    pid_path = state_dir / "agent.pid"
    if os.name == "nt" and pid_path.is_file():
        try:
            from inferyard.node.service_supervisor import terminate_tree

            existing = json.loads(pid_path.read_text(encoding="utf-8"))
            pid, ticks = existing.get("pid"), existing.get("start_ticks")
            if isinstance(pid, int) and isinstance(ticks, int):
                terminate_tree(pid, ticks, graceful_timeout=2)
        except (OSError, ValueError):
            pass
    pid_path.unlink(missing_ok=True)


class AutostartManager:
    def __init__(
        self,
        state_dir: Path,
        *,
        system: str | None = None,
        runner=subprocess.run,
        directories: dict[str, Path] | None = None,
        registry_key: str | None = None,
        spawner=None,
    ):
        self.state_dir = state_dir
        self.system = system or platform.system()
        self.runner = runner
        self._explicit_directories = directories is not None
        self.directories = dict(directories) if directories is not None else {}
        self.registry_key = registry_key
        self.spawner = spawner

    def _directory(self, key: str, default: Path) -> Path:
        return self.directories.get(key, default)

    def install(self, *, activate: bool = True) -> dict:
        self.state_dir.mkdir(parents=True, exist_ok=True)
        if self.system == "Darwin":
            root = self._directory(
                "launch_agents", Path.home() / "Library" / "LaunchAgents"
            )
            path = root / "io.inferyard.agent.plist"
            root.mkdir(parents=True, exist_ok=True)
            path.write_bytes(plistlib.dumps(launch_agent(self.state_dir), sort_keys=False))
            if activate:
                self.runner(
                    ["launchctl", "bootstrap", f"gui/{_uid()}", str(path)], check=True
                )
            return {"mode": "launch_agent", "path": str(path)}
        if self.system == "Windows":
            key = windows_run_key(self.registry_key)
            command = windows_run_command(self.state_dir, registry_key=key)
            pid = None
            if activate:
                self.runner(command, check=True, capture_output=True, timeout=30)
                if self.spawner is not None:
                    pid = self.spawner(self.state_dir)
                elif self.runner is subprocess.run:
                    pid = _start_windows_background(self.state_dir)
            return {
                "mode": "registry_run",
                "key": key,
                "value": WINDOWS_RUN_VALUE,
                "command": command,
                "pid": pid,
            }
        if self.system != "Linux":
            raise ValueError("unsupported autostart platform")
        xdg_config = (
            Path(os.environ["XDG_CONFIG_HOME"])
            if os.environ.get("XDG_CONFIG_HOME")
            else Path.home() / ".config"
        )
        systemd_root = self._directory("systemd", xdg_config / "systemd" / "user")
        has_systemd = "systemd" in self.directories or (
            not self._explicit_directories
            and "fallback" not in self.directories
            and Path("/run/systemd/system").is_dir()
            and shutil.which("systemctl") is not None
        )
        if has_systemd:
            path = systemd_root / "inferyard-agent.service"
            systemd_root.mkdir(parents=True, exist_ok=True)
            path.write_text(systemd_unit(self.state_dir), encoding="utf-8")
            if activate:
                self.runner(["systemctl", "--user", "daemon-reload"], check=True)
                self.runner(["systemctl", "--user", "enable", "--now", path.name], check=True)
            return {"mode": "systemd_user", "path": str(path)}
        path = self._directory("fallback", self.state_dir) / "start-agent.sh"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(fallback_script(self.state_dir), encoding="utf-8")
        path.chmod(0o700)
        if activate:
            self.runner([str(path)], check=True)
        return {"mode": "background_script", "path": str(path)}

    def uninstall(self, *, activate: bool = True) -> dict:
        if self.system == "Darwin":
            path = self._directory(
                "launch_agents", Path.home() / "Library" / "LaunchAgents"
            ) / "io.inferyard.agent.plist"
            if activate and path.exists():
                self.runner(["launchctl", "bootout", f"gui/{_uid()}", str(path)], check=False)
            path.unlink(missing_ok=True)
        elif self.system == "Windows":
            key = windows_run_key(self.registry_key)
            command = ["reg", "delete", key, "/v", WINDOWS_RUN_VALUE, "/f"]
            if activate:
                self.runner(command, check=False, capture_output=True, timeout=30)
                _stop_windows_background(self.state_dir)
            path = None
        elif self.system == "Linux":
            xdg_config = (
                Path(os.environ["XDG_CONFIG_HOME"])
                if os.environ.get("XDG_CONFIG_HOME")
                else Path.home() / ".config"
            )
            service = self._directory(
                "systemd", xdg_config / "systemd" / "user"
            ) / "inferyard-agent.service"
            script = self._directory("fallback", self.state_dir) / "start-agent.sh"
            if activate and service.exists():
                self.runner(["systemctl", "--user", "disable", "--now", service.name], check=False)
            service.unlink(missing_ok=True)
            script.unlink(missing_ok=True)
            path = service
        else:
            raise ValueError("unsupported autostart platform")
        (self.state_dir / "autostart.json").unlink(missing_ok=True)
        return {"removed": True, "path": str(path) if path else None}


def installation_marker(state_dir: Path, result: dict) -> None:
    path = state_dir / "autostart.json"
    path.write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    if os.name != "nt":
        path.chmod(0o600)
