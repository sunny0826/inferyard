"""Read-only onboarding discovery with progress-specific input failure mapping."""

import sys

from inferyard.application.types import CommandResult
from inferyard.config.preparation_io import PreparationError
from inferyard.contracts.validation import ContractError
from inferyard.evidence.formats import UnsupportedFormat
from inferyard.evidence.storage import EvidenceError


def execute(request):
    from inferyard.config.workspace_progress import inspect_workspace

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
