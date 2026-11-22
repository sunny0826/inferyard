"""Application handler for read-only local engine detection and installation init."""

from __future__ import annotations

import os
import sys
from pathlib import Path

from inferyard.application.types import CommandResult
from inferyard.contracts.error_reasons import safe_reason
from inferyard.errors import ContractError, PreflightError


def _detect(request) -> tuple[int, CommandResult]:
    from inferyard.platforms.local_engines.detect import detect_local_engines

    try:
        details = detect_local_engines(extra_paths=getattr(request, "extra_paths", None) or ())
    except OSError:
        print("engine detect failed: io_error", file=sys.stderr)
        return 4, CommandResult(request.command, "error", limitations=("io_error",))
    return 0, CommandResult(request.command, "detected", "complete", details=details)


def _init_installation(request) -> tuple[int, CommandResult]:
    from inferyard.platforms.local_engines.detect import build_installation, write_installation
    from inferyard.platforms.local_engines.installation import (
        check_family_platform,
        freeze_installation,
    )

    family = getattr(request, "family", None)
    executable = getattr(request, "executable", None)
    out = getattr(request, "out", None)
    if not family or executable is None or out is None:
        return 2, CommandResult(request.command, "blocked", limitations=("invalid_input",))
    try:
        if os.path.lexists(Path(out).absolute()):
            raise FileExistsError("output_exists")
        declaration = build_installation(
            family,
            executable,
            working_directory=getattr(request, "working_directory", None),
            runtime_roots=getattr(request, "runtime_roots", None),
            models_root=getattr(request, "models_root", None),
            data_dir=getattr(request, "data_dir", None),
        )
        verified = False
        try:
            check_family_platform(family)
        except PreflightError:
            supported = False
        else:
            supported = True
        if supported:
            freeze_installation(declaration)
            verified = True
        written = write_installation(out, declaration)
    except FileExistsError:
        return 2, CommandResult(request.command, "blocked", limitations=("output_exists",))
    except PreflightError as exc:
        reason = safe_reason(exc, "preflight_blocked")
        return 2, CommandResult(request.command, "blocked", limitations=(reason,))
    except ContractError:
        return 2, CommandResult(request.command, "blocked", limitations=("invalid_input",))
    except OSError:
        print("engine installation init failed: io_error", file=sys.stderr)
        return 4, CommandResult(request.command, "error", limitations=("io_error",))
    return 0, CommandResult(
        request.command,
        "prepared",
        "complete",
        details={
            "definition": "local-engine-init.v1",
            "out": written,
            "family": declaration["family"],
            "installation": declaration,
            "verified": verified,
            "model_requests_sent": 0,
        },
    )


def execute(request) -> tuple[int, CommandResult]:
    if request.command == "engine detect":
        return _detect(request)
    if request.command == "engine init-installation":
        return _init_installation(request)
    return 2, CommandResult(request.command, "blocked", limitations=("invalid_input",))
