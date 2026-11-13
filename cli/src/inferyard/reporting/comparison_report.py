"""Offline pair comparison JSON; raw source runs are never changed."""

from copy import deepcopy

from inferyard import SCHEMA_VERSION
from inferyard.analysis.comparison import compare_trials
from inferyard.analysis.comparison_input import comparison_input
from inferyard.contracts.format_versions import require_version
from inferyard.contracts.validation import json_bytes
from inferyard.errors import EvidenceError
from inferyard.evidence.formats import require_core
from inferyard.evidence.storage import atomic_bytes, local_file, new_output, read_json

FORMAT_VERSION = 5


def build_comparison(
    left_path,
    right_path,
    mode=None,
    *,
    left_overhead=None,
    right_overhead=None,
    loaded=None,
    format_version=FORMAT_VERSION,
    source_roots=(),
):
    require_version({"version": format_version}, "version", (FORMAT_VERSION,), "comparison")
    if (left_overhead is None) != (right_overhead is None):
        raise EvidenceError("both_performance_overhead_sources_required")
    loaded = (
        loaded
        if loaded is not None
        else [comparison_input(left_path), comparison_input(right_path)]
    )
    (left, left_ref), (right, right_ref) = loaded
    evidence = None
    if left_overhead is not None:
        from inferyard.reporting.total_applicability import load_performance_evidence_v3

        evidence = [
            load_performance_evidence_v3(
                root,
                target,
                data=data,
                reference=reference,
                **({"source_roots": source_roots} if source_roots else {}),
            )
            for root, target, (data, reference) in zip(
                (left_overhead, right_overhead), (left_path, right_path), loaded, strict=True
            )
        ]
        for proof, ref in zip(evidence, (left_ref, right_ref), strict=True):
            if proof["target_manifest_sha256"] != ref["manifest_sha256"]:
                raise EvidenceError("performance_target_changed_during_read")
    result = compare_trials(
        left,
        right,
        mode=mode,
        performance_evidence=evidence,
        definition="phase2.v3",
    )
    result["schema_version"] = SCHEMA_VERSION
    result["format_version"] = format_version
    if evidence is not None:
        result["performance_evidence"] = evidence
    result["requested_mode"] = mode
    result["source_runs"] = [dict(left_ref), dict(right_ref)]
    result["difference_direction"] = "right_minus_left"
    result["sides"] = [
        {
            "run_id": data["run"]["run_id"],
            **{
                key: data["summary"][key]
                for key in ("completeness", "counts", "completion_rate", "quality", "performance")
            },
        }
        for data in (left, right)
    ]
    return result


def _require_only_comparison(root):
    """A current comparison owns exactly one file; retired presentation files conflict."""
    names = {entry.name for entry in root.iterdir()}
    if names != {"comparison.json"}:
        raise EvidenceError("comparison_format_invalid")


def read_verified_comparison(root, *, loaded=None, source_roots=()):
    saved = read_json(local_file(root, "comparison.json"))
    if (
        type(saved) is not dict
        or type(saved.get("schema_version")) is not int
        or type(saved.get("format_version")) is not int
        or saved.get("schema_version") != SCHEMA_VERSION
        or saved.get("format_version") != FORMAT_VERSION
    ):
        # Retired presentation layouts are checked for exact sealed bytes before
        # their version is rejected; a damaged seal can never become a version error.
        from inferyard.evidence.legacy_presentation_seal import legacy_index_bytes

        legacy_index_bytes(root)
    require_core(saved, "comparison")
    require_version(saved, "format_version", (FORMAT_VERSION,), "comparison")
    _require_only_comparison(root)
    try:
        sources = saved["source_runs"]
        if len(sources) != 2:
            raise EvidenceError("comparison_format_invalid")
        from inferyard.evidence.source_locations import resolve_source

        paths = [resolve_source(root, ref["path"], source_roots) for ref in sources]
        loaded = (
            loaded
            if loaded is not None
            else [comparison_input(path, bind_unsealed=True) for path in paths]
        )
        actual = [{**ref, "path": sources[i]["path"]} for i, (_, ref) in enumerate(loaded)]
        if sources != actual:
            raise EvidenceError("report_comparison_sources_or_order_mismatch")
        options = {}
        if "performance_evidence" in saved:
            proofs = saved["performance_evidence"]
            if len(proofs) != 2:
                raise EvidenceError("comparison_format_invalid")
            options = {
                key: resolve_source(root, proof["source"]["path"], source_roots)
                for key, proof in zip(("left_overhead", "right_overhead"), proofs, strict=True)
            }
        expected = build_comparison(
            paths[0],
            paths[1],
            saved["requested_mode"],
            loaded=loaded,
            format_version=saved["format_version"],
            source_roots=source_roots,
            **options,
        )
        from inferyard.evidence.source_locations import retain_comparison_locations

        retain_comparison_locations(expected, saved)
    except (KeyError, TypeError, IndexError) as exc:
        raise EvidenceError("comparison_format_invalid") from exc
    if json_bytes(saved) != json_bytes(expected):
        raise EvidenceError("comparison_recomputation_mismatch")
    return expected


def verify_comparison(root, *, source_roots=()):
    read_verified_comparison(root, source_roots=source_roots)
    return {"verified": True, "source_runs": 2}


def write_comparison(request):
    """Write one comparison JSON document; no index, HTML or presentation seal."""
    source_metadata = {}
    loaded = [
        comparison_input(
            path,
            bind_unsealed=True,
            metadata=source_metadata.setdefault(path.resolve(), {}),
        )
        for path in (request.left, request.right)
    ]
    result = build_comparison(
        request.left,
        request.right,
        request.comparison_mode,
        left_overhead=request.left_overhead,
        right_overhead=request.right_overhead,
        loaded=loaded,
        format_version=FORMAT_VERSION,
    )
    from inferyard.evidence.source_locations import relative_comparison

    sources = [request.left, request.right]
    sources.extend(p for p in (request.left_overhead, request.right_overhead) if p is not None)
    stored = deepcopy(result)
    relative_comparison(stored, request.out)
    new_output(request.out, sources)
    atomic_bytes(request.out / "comparison.json", json_bytes(stored))
    return result
