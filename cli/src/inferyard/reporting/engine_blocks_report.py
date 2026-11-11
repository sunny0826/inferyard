"""`compare-engines`: interleaved engine blocks into a sealed engine-comparison.v1 artifact."""

import hashlib
from copy import deepcopy
from importlib.resources import files

from inferyard.application.types import CommandResult
from inferyard.contracts.validation import strict_json_loads, json_bytes
from inferyard.errors import EvidenceError
from inferyard.evidence.storage import atomic_bytes, new_output

RESULT = "engine-comparison.json"
HTML = "report.html"
TEMPLATE = "engine_comparison.html"
SEALED = (RESULT, HTML)


def template_sha256():
    raw = files("inferyard").joinpath("templates", TEMPLATE).read_bytes()
    return hashlib.sha256(raw).hexdigest()


def load_blocks(paths):
    from inferyard.analysis.first_body import attach_first_body
    from inferyard.reporting.comparison_report import comparison_input

    if len({path.resolve() for path in paths}) != len(paths):
        raise EvidenceError("duplicate_engine_comparison_run")
    blocks, sources = [], []
    for path in paths:
        metadata = {}
        data, reference = comparison_input(path, bind_unsealed=True, metadata=metadata)
        attach_first_body(data, metadata["evidence"].jsonl("events.jsonl")[0])
        blocks.append(data)
        sources.append(reference)
    return blocks, sources


def build(paths, out, *, producer=None, loaded=None):
    """Result with source paths relative to `out`; producer identity is provenance only."""
    from inferyard.analysis.engine_blocks import compare_blocks
    from inferyard.evidence.source_locations import relative_source
    from inferyard.provenance import tool_source_hash

    blocks, sources = loaded if loaded is not None else load_blocks(paths)
    result = compare_blocks(blocks)
    result["source_runs"] = [
        {**ref, "path": relative_source(ref["path"], out.resolve())} for ref in deepcopy(sources)
    ]
    result["producer"] = producer or {
        "tool_source_sha256": tool_source_hash(),
        "template_sha256": template_sha256(),
    }
    return result


def render(result):
    from inferyard.reporting.report_common import _environment

    return _environment().get_template(TEMPLATE).render(result=result)


def execute(request, *, options=None):
    if request.command == "engine-compare-check":
        from inferyard.application.types import VerificationOptions
        from inferyard.reporting.engine_blocks_verify import verify

        options = options or VerificationOptions.from_request(request)
        details = verify(request.run, options=options)
        return 0, CommandResult(request.command, "verified", "complete", details=details)
    from inferyard.evidence.artifact_seal import seal

    paths = [*(request.left_runs or ()), *(request.right_runs or ())]
    result = build(paths, request.out)
    new_output(request.out, paths)
    raw = json_bytes(result)
    atomic_bytes(request.out / RESULT, raw)
    # Render the saved form so verify --rerender sees the same key order.
    atomic_bytes(request.out / HTML, render(strict_json_loads(raw.decode())).encode())
    seal(request.out, SEALED)
    return 0, CommandResult(
        request.command,
        "compared",
        "complete",
        evidence_dir=str(request.out),
        limitations=tuple(result["limitations"]),
        details=result,
    )
