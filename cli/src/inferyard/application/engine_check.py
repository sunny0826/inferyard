"""Read-only engine verification; a pass never confers candidate eligibility."""

import stat
import sys

from inferyard.application.types import CommandResult
from inferyard.config.preparation_io import PreparationError, require_platform, sha256
from inferyard.contracts.schemas_engine_check import REASONS
from inferyard.evidence.storage import json_bytes
from inferyard.platforms.runtime_profiles import RELEASE


def _reported(version):
    # The first non-empty output line is the engine's own version report; the
    # frozen pattern only judges it, it never fabricates a value.
    for line in (*version["stdout"].splitlines(), *version["stderr"].splitlines()):
        if line.strip():
            return line.strip()
    return None


def _input_file(path, *, resolve):
    # The engine path is resolved exactly like config create resolves it; the
    # receipt path is handed to the shared verifier unresolved so the symlink
    # policy (runtime_verification.linked) stays in one place instead of being
    # silently bypassed by a pre-resolved path.
    try:
        resolved = path.resolve(strict=True)
        if not stat.S_ISREG(resolved.stat().st_mode):
            raise PreparationError("invalid_input")
    except (FileNotFoundError, NotADirectoryError, IsADirectoryError) as exc:
        raise PreparationError("invalid_input") from exc
    return resolved if resolve else path


def _verify(request):
    from inferyard.config.community_assets import libraries
    from inferyard.platforms import runtime_verification
    from inferyard.platforms.runtime_profiles import VERSION_PATTERN

    system, _ = require_platform(("Darwin", "arm64"), ("Windows", "x64"))
    if (
        system == "Darwin" and (request.engine_path is None or request.runtime_receipt is not None)
    ) or (
        system == "Windows" and (request.runtime_receipt is None or request.engine_path is not None)
    ):
        raise PreparationError("invalid_input")
    modes, mode = None, None
    if system == "Darwin":
        engine_path = _input_file(request.engine_path, resolve=True)
        library_map = libraries(engine_path, ("*.dylib",))
        if len(library_map) == 1:
            raise PreparationError("invalid_library_manifest")
    else:
        profile, engine_path, _ = runtime_verification.verify_receipt(
            _input_file(request.runtime_receipt, resolve=False)
        )
        mode = profile["mode"]
    version = runtime_verification.query_version(engine_path, VERSION_PATTERN)
    if system == "Darwin":
        # Without a device preflight there is no selected mode (model size can
        # flip the device-check recommendation), so each mode gets its own
        # result; the metal requirement stays a per-mode outcome here and the
        # final decision stays with config create.
        modes = {"cpu": True, "metal": "libggml-metal.dylib" in library_map}
    return {
        "eligible": True,
        "reason": None,
        "reported_version": _reported(version),
        "expected_release": RELEASE,
        "modes": modes,
        "mode": mode,
    }


def execute(request):
    if request.installation is not None:
        from inferyard.platforms.identity import PreflightError
        from inferyard.platforms.local_engines import (
            freeze_installation,
            read_installation,
            verify_installation,
        )

        installation = None
        try:
            installation = read_installation(request.installation)
            snapshot = freeze_installation(installation)
            verify_installation(snapshot)
        except PreflightError as exc:
            return 0, CommandResult(
                request.command,
                "checked",
                "complete",
                details={
                    "definition": "local-engine-check.v2",
                    "eligible": False,
                    "reason": str(exc),
                    "family": installation["family"] if installation else None,
                    "declaration_sha256": None,
                    "verification_scope": "installation_runtime_closure_only",
                    "model_requests_sent": 0,
                    "ready_to_run": False,
                },
            )
        return 0, CommandResult(
            request.command,
            "checked",
            "complete",
            details={
                "definition": "local-engine-check.v2",
                "eligible": True,
                "reason": None,
                "family": installation["family"],
                "declaration_sha256": sha256(json_bytes(installation)),
                "verification_scope": "installation_runtime_closure_only",
                "model_requests_sent": 0,
                "ready_to_run": False,
            },
        )
    try:
        details = _verify(request)
    except PreparationError as exc:
        if exc.code == 2 and exc.reason in REASONS:
            # Ineligibility is a successful verification, not an input block:
            # unlike the device-check family (blocked -> exit 2), this query
            # never blocks follow-up operations; config create is the final
            # authority.
            print(f"engine ineligible: {exc.reason}", file=sys.stderr)
            return 0, CommandResult(
                request.command,
                "checked",
                "complete",
                details={
                    "eligible": False,
                    "reason": exc.reason,
                    "reported_version": None,
                    "expected_release": RELEASE,
                    "modes": None,
                    "mode": None,
                },
            )
        print(f"engine check failed: {exc.reason}", file=sys.stderr)
        return exc.code, CommandResult(
            request.command, "blocked" if exc.code == 2 else "error", limitations=(exc.reason,)
        )
    return 0, CommandResult(request.command, "checked", "complete", details=details)
