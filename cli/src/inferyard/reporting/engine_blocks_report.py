"""`compare-engines`: interleaved same-GGUF engine blocks into engine-comparison.v1."""

from inferyard.application.types import CommandResult
from inferyard.evidence.storage import EvidenceError, atomic_bytes, json_bytes


def execute(request):
    from inferyard.analysis.engine_blocks import compare_blocks
    from inferyard.reporting.comparison_report import comparison_input
    from inferyard.reporting.report_common import _new_output

    paths = [*(request.left_runs or ()), *(request.right_runs or ())]
    if len({path.resolve() for path in paths}) != len(paths):
        raise EvidenceError("duplicate_engine_comparison_run")
    blocks, sources = [], []
    for path in paths:
        data, reference = comparison_input(path, bind_unsealed=True)
        blocks.append(data)
        sources.append(reference)
    result = compare_blocks(blocks)
    result["source_runs"] = sources
    _new_output(request.out, paths)
    atomic_bytes(request.out / "engine-comparison.json", json_bytes(result))
    return 0, CommandResult(
        request.command,
        "compared",
        "complete",
        evidence_dir=str(request.out),
        limitations=tuple(result["limitations"]),
        details=result,
    )
