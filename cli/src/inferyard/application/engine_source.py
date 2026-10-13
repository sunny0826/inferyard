"""Preparation-only engine acquisition results with fixed, credential-safe reasons."""

import sys

from inferyard.application.types import CommandResult
from inferyard.config.preparation_io import PreparationError


def execute(request):
    from inferyard.platforms.engine_source_download import acquire

    try:
        details = acquire(
            request.engine_source_url,
            request.out,
            timeout_seconds=request.engine_timeout_seconds,
        )
    except PreparationError as exc:
        print(f"engine acquisition failed: {exc.reason}", file=sys.stderr)
        status = "interrupted" if exc.code == 130 else "blocked" if exc.code == 2 else "error"
        return exc.code, CommandResult(request.command, status, limitations=(exc.reason,))
    return 0, CommandResult(request.command, "prepared", "complete", details=details)
