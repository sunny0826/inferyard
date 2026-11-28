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


def windows_task_command(state_dir: Path) -> list[str]:
    command = subprocess.list2cmdline(agent_command(state_dir, windows=True))
    return [
        "schtasks",
        "/Create",
        "/TN",
        "InferYardAgent",
        "/SC",
        "ONLOGON",
        "/RL",
        "LIMITED",
        "/F",
        "/TR",
        command,
    ]


class AutostartManager:
    def __init__(
        self,
        state_dir: Path,
        *,
        system: str | None = None,
        runner=subprocess.run,
        directories: dict[str, Path] | None = None,
    ):
        self.state_dir = state_dir
        self.system = system or platform.system()
        self.runner = runner
        self.directories = directories or {}

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
                    ["launchctl", "bootstrap", f"gui/{os.getuid()}", str(path)], check=True
                )
            return {"mode": "launch_agent", "path": str(path)}
        if self.system == "Windows":
            command = windows_task_command(self.state_dir)
            if activate:
                self.runner(command, check=True, capture_output=True, timeout=30)
                self.runner(
                    ["schtasks", "/Run", "/TN", "InferYardAgent"],
                    check=True,
                    capture_output=True,
                    timeout=30,
                )
            return {"mode": "scheduled_task", "task": "InferYardAgent", "command": command}
        if self.system != "Linux":
            raise ValueError("unsupported autostart platform")
        systemd_root = self._directory("systemd", Path.home() / ".config" / "systemd" / "user")
        has_systemd = "systemd" in self.directories or (
            Path("/run/systemd/system").is_dir() and shutil.which("systemctl") is not None
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
                self.runner(["launchctl", "bootout", f"gui/{os.getuid()}", str(path)], check=False)
            path.unlink(missing_ok=True)
        elif self.system == "Windows":
            command = ["schtasks", "/Delete", "/TN", "InferYardAgent", "/F"]
            if activate:
                self.runner(command, check=False, capture_output=True, timeout=30)
            path = None
        elif self.system == "Linux":
            service = self._directory(
                "systemd", Path.home() / ".config" / "systemd" / "user"
            ) / "inferyard-agent.service"
            script = self._directory("fallback", self.state_dir) / "start-agent.sh"
            if activate and service.exists():
                self.runner(["systemctl", "--user", "disable", "--now", service.name], check=False)
            service.unlink(missing_ok=True)
            script.unlink(missing_ok=True)
            path = service
        else:
            raise ValueError("unsupported autostart platform")
        return {"removed": True, "path": str(path) if path else None}


def installation_marker(state_dir: Path, result: dict) -> None:
    path = state_dir / "autostart.json"
    path.write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    if os.name != "nt":
        path.chmod(0o600)
