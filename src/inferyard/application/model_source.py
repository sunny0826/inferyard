"""Preparation-only acquisition results with fixed, credential-safe reasons."""

import sys

from inferyard.application.types import CommandResult
from inferyard.config.preparation_io import PreparationError


def execute(request):
    from inferyard.platforms.model_source_host import acquire

    try:
        details = acquire(request.model_source_url, request.out, request.model_token_env)
    except PreparationError as exc:
        print(f"model acquisition failed: {exc.reason}", file=sys.stderr)
        status = "interrupted" if exc.code == 130 else "blocked" if exc.code == 2 else "error"
        return exc.code, CommandResult(request.command, status, limitations=(exc.reason,))
    return 0, CommandResult(request.command, "prepared", "complete", details=details)
