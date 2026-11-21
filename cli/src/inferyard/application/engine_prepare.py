"""Preparation-only engine download and extraction with fixed, credential-safe reasons."""

import sys

from inferyard.application.types import CommandResult
from inferyard.provisioning.preparation_io import PreparationError


def execute(request):
    from inferyard.provisioning.engine_prepare import prepare

    try:
        details = prepare(
            request.engine_source_url,
            request.out,
            expected_sha256=getattr(request, "expected_sha256", None),
            runtime_profile=getattr(request, "runtime_profile", None),
            runtime_url=getattr(request, "runtime_source_url", None),
            runtime_expected_sha256=getattr(request, "runtime_expected_sha256", None),
            timeout_seconds=request.engine_timeout_seconds,
        )
    except PreparationError as exc:
        cause = exc.details.get("failure_cause") if isinstance(exc.details, dict) else None
        suffix = f" ({cause})" if cause else ""
        print(f"engine preparation failed: {exc.reason}{suffix}", file=sys.stderr)
        status = "interrupted" if exc.code == 130 else "blocked" if exc.code == 2 else "error"
        return exc.code, CommandResult(
            request.command,
            status,
            limitations=(exc.reason,),
            details=exc.details if exc.details is not None else None,
        )
    return 0, CommandResult(request.command, "prepared", "complete", details=details)
