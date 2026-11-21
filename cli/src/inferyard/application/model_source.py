"""Preparation-only acquisition results with fixed, credential-safe reasons."""

import sys

from inferyard.application.types import CommandResult
from inferyard.provisioning.preparation_io import PreparationError


def execute(request):
    from inferyard.provisioning.model_source_download import acquire

    try:
        details = acquire(
            request.model_source_url,
            request.out,
            request.model_token_env,
            timeout_seconds=request.model_timeout_seconds,
            progress_stream=getattr(request, "progress_stream", False),
            resume=getattr(request, "resume_transfer", False),
            proxy_origin=getattr(request, "proxy_origin", None),
        )
    except PreparationError as exc:
        cause = exc.details.get("failure_cause") if isinstance(exc.details, dict) else None
        suffix = f" ({cause})" if cause else ""
        print(f"model acquisition failed: {exc.reason}{suffix}", file=sys.stderr)
        status = "interrupted" if exc.code == 130 else "blocked" if exc.code == 2 else "error"
        return exc.code, CommandResult(
            request.command,
            status,
            limitations=(exc.reason,),
            details=exc.details if exc.details is not None else None,
        )
    return 0, CommandResult(request.command, "prepared", "complete", details=details)
