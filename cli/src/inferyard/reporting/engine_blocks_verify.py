"""Verify a sealed engine-comparison.v1 artifact by recomputing it from its source runs."""

from inferyard.contracts.validation import strict_json_loads, json_bytes
from inferyard.errors import ContractError, EvidenceError
from inferyard.evidence.artifact_seal import read_sealed
from inferyard.evidence.source_locations import resolve_source


def _hash(value):
    return type(value) is str and len(value) == 64 and all(c in "0123456789abcdef" for c in value)


def _saved(raw):
    from inferyard.analysis.engine_blocks import DEFINITION

    try:
        saved = strict_json_loads(raw.decode("utf-8"))
    except (UnicodeError, ContractError) as exc:
        raise EvidenceError("engine_comparison_format_invalid") from exc
    if (
        type(saved) is not dict
        or saved.get("definition") != DEFINITION
        or type(saved.get("source_runs")) is not list
        or not saved["source_runs"]
        or any(type(ref) is not dict for ref in saved["source_runs"])
        or type(saved.get("producer")) is not dict
        or set(saved["producer"]) != {"tool_source_sha256", "template_sha256"}
        or not all(_hash(value) for value in saved["producer"].values())
    ):
        raise EvidenceError("engine_comparison_format_invalid")
    return saved


def verify(root, *, options):
    from inferyard.reporting.engine_blocks_report import HTML, RESULT, SEALED, build, render

    blobs = read_sealed(root, SEALED, retain={RESULT, *([HTML] if options.rerender else [])})
    saved = _saved(blobs[RESULT])
    paths = [
        resolve_source(root, ref.get("path"), options.source_roots) for ref in saved["source_runs"]
    ]
    # Producer identity is provenance, not authority for the source-derived result.
    expected = build(paths, root, producer=saved["producer"])
    if len(expected["source_runs"]) != len(saved["source_runs"]):
        raise EvidenceError("engine_comparison_sources_mismatch")
    for ref, stored in zip(expected["source_runs"], saved["source_runs"], strict=True):
        ref["path"] = stored["path"]
    if json_bytes(expected) != json_bytes(saved):
        raise EvidenceError("engine_comparison_recomputation_mismatch")
    if options.rerender and render(saved).encode() != blobs[HTML]:
        raise EvidenceError("engine_comparison_html_recomputation_mismatch")
    return {
        "verified": True,
        "source_runs": len(paths),
        "bytes_verified": True,
        "sources_verified": True,
        "semantic_verified": True,
        "render_checked": options.rerender,
    }
