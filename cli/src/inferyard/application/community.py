"""Dispatch installed preparation actions and map their fixed failure reasons."""

import sys

from inferyard.application.types import CommandResult
from inferyard.config.preparation_io import PreparationError


def execute(request):
    try:
        if request.command == "init":
            from inferyard.config.community_workspace import initialize

            details = initialize(request)
        elif request.command == "runtime prepare":
            from inferyard.platforms.windows_runtime_prepare import prepare

            details = prepare(request)
        elif request.command == "config assets":
            from inferyard.config.community_assets import prepare

            details = prepare(request)
        elif request.command == "config create":
            from inferyard.config.community_candidates import create

            details = create(request)
        else:
            from inferyard.config.service_binding import prepare

            details = prepare(request)
    except PreparationError as exc:
        print(f"preparation failed: {exc.reason}", file=sys.stderr)
        return exc.code, CommandResult(
            request.command, "blocked" if exc.code == 2 else "error", limitations=(exc.reason,)
        )
    return 0, CommandResult(request.command, "prepared", "complete", details=details)
