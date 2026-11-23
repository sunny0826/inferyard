"""Application handler for managed PolyStrata runtime and model preparation."""

from __future__ import annotations

import sys

from inferyard.application.types import CommandResult
from inferyard.provisioning.preparation_io import PreparationError


def execute(request):
    from inferyard.provisioning.polystrata_prepare import prepare_polystrata

    try:
        details = prepare_polystrata(
            request.out,
            data_dir=getattr(request, "data_dir", None),
            family=getattr(request, "family", None),
            model=getattr(request, "polystrata_model", None),
            context=getattr(request, "context_size", None),
            backend=getattr(request, "polystrata_backend", None),
            model_source=getattr(request, "polystrata_model_source", None),
            gguf_path=getattr(request, "gguf_path", None),
            source=getattr(request, "engine_source_url", None),
            expected_sha256=getattr(request, "expected_sha256", None),
            proxy_origin=getattr(request, "proxy_origin", None),
            resume=getattr(request, "resume_transfer", False),
            progress_stream=getattr(request, "progress_stream", False),
            timeout_seconds=request.engine_timeout_seconds,
            venv_python=getattr(request, "polystrata_python", None),
        )
    except PreparationError as exc:
        cause = exc.details.get("failure_cause") if isinstance(exc.details, dict) else None
        suffix = f" ({cause})" if cause and cause != exc.reason else ""
        print(f"polystrata preparation failed: {exc.reason}{suffix}", file=sys.stderr)
        status = "interrupted" if exc.code == 130 else "blocked" if exc.code == 2 else "error"
        return exc.code, CommandResult(
            request.command,
            status,
            limitations=(exc.reason,),
            details=exc.details if exc.details is not None else None,
        )
    return 0, CommandResult(request.command, "prepared", "complete", details=details)
