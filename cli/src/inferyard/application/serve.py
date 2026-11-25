"""Dispatch standalone serve preparation commands."""

import sys

from inferyard.application.types import CommandResult
from inferyard.provisioning.preparation_io import PreparationError


def execute(request):
    try:
        if request.command == "serve prepare":
            from inferyard.config.serve_plan import prepare

            details = prepare(request)
        else:
            raise PreparationError("invalid_input")
    except PreparationError as exc:
        print(f"preparation failed: {exc.reason}", file=sys.stderr)
        return exc.code, CommandResult(
            request.command, "blocked" if exc.code == 2 else "error", limitations=(exc.reason,)
        )
    return 0, CommandResult(request.command, "prepared", "complete", details=details)
