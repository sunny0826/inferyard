"""Lazy application dispatch for remote node commands."""

from __future__ import annotations

import asyncio

from inferyard.application.types import CommandRequest, CommandResult

NODE_COMMANDS = frozenset({"join", "agent run", "agent install-autostart", "agent uninstall-autostart"})


def _state_dir(request: CommandRequest):
    from inferyard.node.credentials import default_state_dir

    return request.state_dir or default_state_dir()


def execute(request: CommandRequest) -> tuple[int, CommandResult]:
    if request.command == "join":
        from inferyard.node.join import PairingError, join

        try:
            details = join(
                hubs=request.hubs or (),
                code=request.code or "",
                ca_sha256=request.ca_sha256 or "",
                state_dir=_state_dir(request),
                no_autostart=request.no_autostart,
                source_hint=request.source_hint,
            )
        except PairingError as exc:
            return 2, CommandResult(request.command, "blocked", limitations=(exc.reason,))
        return 0, CommandResult(request.command, "paired", "complete", details=details)
    if request.command == "agent run":
        from inferyard.node.agent import run_agent

        asyncio.run(run_agent(_state_dir(request), hubs=request.hubs))
        return 0, CommandResult(request.command, "stopped", "complete")
    from inferyard.node.autostart import AutostartManager, installation_marker

    manager = AutostartManager(_state_dir(request))
    if request.command == "agent install-autostart":
        details = manager.install()
        installation_marker(_state_dir(request), details)
        return 0, CommandResult(request.command, "installed", "complete", details=details)
    if request.command == "agent uninstall-autostart":
        details = manager.uninstall()
        return 0, CommandResult(request.command, "uninstalled", "complete", details=details)
    raise ValueError("unsupported node command")
