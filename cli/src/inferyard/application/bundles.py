"""Application handlers for benchmark bundle listing and verification."""

from inferyard.application.types import CommandResult
from inferyard.bundles.registry import list_bundles, verify_bundles


def execute(request):
    if request.command == "bundles list":
        return 0, CommandResult(
            "bundles list", "listed", "complete", details=list_bundles()
        )
    if request.command == "bundles verify":
        return 0, CommandResult(
            "bundles verify",
            "verified",
            "complete",
            details=verify_bundles(request.bundle_target),
        )
    raise ValueError(f"unsupported bundles command: {request.command}")
