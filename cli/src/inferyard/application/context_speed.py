"""Public preset and offline result handlers; no report or model calls."""

from dataclasses import asdict, replace

from inferyard.application.types import CommandResult
from inferyard.contracts.context_speed import validate_response
from inferyard.contracts.validation import ContractError
from inferyard.evidence.context_speed import NotSpeed
from inferyard.evidence.formats import UnsupportedFormat
from inferyard.evidence.storage import EvidenceError


def execute(request):
    if request.command == "plan":
        from inferyard.config.context_speed_plan import freeze

        plan = freeze(request.config, None if request.dry_run else request.out)
        return 0, CommandResult(
            "plan",
            "previewed" if request.dry_run else "frozen",
            "complete",
            evidence_dir=None if request.dry_run else str(request.out),
            limitations=tuple(plan["limitations"]),
            details=plan,
        )
    try:
        from inferyard.analysis.context_speed import project
        from inferyard.application.raw_verification import execute as verify
        from inferyard.evidence.context_speed import read_inputs
        from inferyard.evidence.ledger import plan_progress

        # Reuse the complete raw batch verifier, including metadata and saved identities.
        verify(replace(request, run=request.batch), "batch")
        plan, loaded, runs = read_inputs(request.batch)
        details = project(request.batch, plan, loaded, runs)
        complete = (
            bool(runs)
            and all(d["manifest_sealed"] for d in runs)
            and all(t["status"] == "complete" for t in plan_progress(plan, runs))
        )
        code = 0 if complete else 3
        result = CommandResult(
            "speed-results",
            "finished" if complete else "partial",
            "complete" if complete else "incomplete",
            evidence_dir=str(request.batch),
            details=details,
        )
    except NotSpeed:
        code, result = error(2, "context_speed_batch_required")
    except UnsupportedFormat:
        code, result = error(2, "unsupported_format")
    except ContractError, EvidenceError, OSError, ValueError, KeyError, TypeError, IndexError:
        code, result = error(4, "context_speed_evidence_invalid")
    response = asdict(result)
    response["limitations"] = list(result.limitations)
    validate_response(response, code)
    return code, result


def error(code, reason):
    return code, CommandResult(
        "speed-results",
        "error",
        details={
            "schema_version": 3,
            "kind": "context_speed_result.v1",
            "read_state": "error",
            "error": {"code": reason, "source_path": None},
        },
    )
