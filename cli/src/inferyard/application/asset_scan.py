"""Read-only local asset scanning command execution with sanitized OS error mapping."""

import sys

from inferyard.application.types import CommandResult


def execute(request):
    """Scan declared model and engine roots without starting processes or sending requests."""
    from inferyard.platforms.asset_scan import scan_assets

    try:
        result = scan_assets(
            request.models_roots or [],
            getattr(request, "engine_roots", None) or (),
        )
    except OSError:
        print("asset scan failed: io_error", file=sys.stderr)
        return 4, CommandResult(request.command, "error", limitations=("io_error",))
    return 0, CommandResult(request.command, "scanned", "complete", details=result)
