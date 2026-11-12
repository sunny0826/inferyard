"""Read-only onboarding discovery with progress-specific input failure mapping."""

import sys

from inferyard.application.types import CommandResult
from inferyard.provisioning.preparation_io import PreparationError
from inferyard.errors import ContractError, EvidenceError, UnsupportedFormat


def execute(request):
    from inferyard.application.workspace_progress import inspect_workspace

    try:
        details = inspect_workspace(request.workspace)
    except (
        PreparationError,
        ContractError,
        UnsupportedFormat,
        EvidenceError,
        OSError,
        ValueError,
        TypeError,
        KeyError,
        AttributeError,
        IndexError,
    ):
        print("progress blocked: workspace_artifact_unreadable", file=sys.stderr)
        return 2, CommandResult(
            "progress", "blocked", limitations=("workspace_artifact_unreadable",)
        )
    return 0, CommandResult("progress", "inspected", "complete", details=details)
