"""Preparation-only engine acquisition results with fixed, credential-safe reasons."""

import sys

from inferyard.application.types import CommandResult
from inferyard.provisioning.preparation_io import PreparationError


def execute(request):
    from inferyard.provisioning.engine_source_download import acquire

    try:
        details = acquire(
            request.engine_source_url,
            request.out,
            timeout_seconds=request.engine_timeout_seconds,
            progress_stream=getattr(request, "progress_stream", False),
            resume=getattr(request, "resume_transfer", False),
        )
    except PreparationError as exc:
        cause = exc.details.get("failure_cause") if isinstance(exc.details, dict) else None
        suffix = f" ({cause})" if cause else ""
        print(f"engine acquisition failed: {exc.reason}{suffix}", file=sys.stderr)
        status = "interrupted" if exc.code == 130 else "blocked" if exc.code == 2 else "error"
        return exc.code, CommandResult(
            request.command,
            status,
            limitations=(exc.reason,),
            details=exc.details if exc.details is not None else None,
        )
    return 0, CommandResult(request.command, "prepared", "complete", details=details)
