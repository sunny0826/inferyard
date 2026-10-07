"""Offline pair comparison; raw source runs are never changed."""

from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.analysis.comparison import compare_trials
from inferyard.application.types import CommandResult, VerificationOptions
from inferyard.contracts.validation import ContractError
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import (
    EvidenceError,
    atomic_bytes,
    json_bytes,
    local_file,
    read_json,
)


def comparison_input(root, *, bind_unsealed=False):
    metadata = {}
    data = read_trial(root, metadata=metadata)
    data.update(
        {key: metadata[key] for key in ("identity", "environment_start", "first_event_utc")}
    )
    ref = {
        "run_id": data["run"]["run_id"],
        "path": str(root.resolve()),
        "manifest_sha256": metadata["manifest_sha256"],
    }
    if bind_unsealed and ref["manifest_sha256"] is None:
        import hashlib

        ref["unsealed_content_sha256"] = hashlib.sha256(json_bytes(data)).hexdigest()
    return data, ref


def build_comparison(
    left_path,
    right_path,
    mode=None,
    *,
    left_overhead=None,
    right_overhead=None,
    loaded=None,
    format_version=3,
    source_roots=(),
):
    if type(format_version) is not int or format_version not in (1, 2, 3, 4):
        raise EvidenceError("comparison_format_invalid")
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
        from inferyard.evidence.performance_evidence import load_performance_evidence

        if format_version >= 3:
            from inferyard.evidence.total_applicability import load_performance_evidence_v3

            evidence = [
                load_performance_evidence_v3(
                    root,
                    target,
                    data=data,
                    reference=reference,
                    **(
                        {"source_roots": source_roots}
                        if format_version == 4 and source_roots
                        else {}
                    ),
                )
                for root, target, (data, reference) in zip(
                    (left_overhead, right_overhead), (left_path, right_path), loaded, strict=True
                )
            ]
        else:
            evidence = [
                load_performance_evidence(root, target)
                for root, target in ((left_overhead, left_path), (right_overhead, right_path))
            ]
        for proof, ref in zip(evidence, (left_ref, right_ref), strict=True):
            if proof["target_manifest_sha256"] != ref["manifest_sha256"]:
                raise EvidenceError("performance_target_changed_during_read")
    result = compare_trials(
        left,
        right,
        mode=mode,
        performance_evidence=evidence,
        definition=f"phase2.v{min(format_version, 3)}",
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


def read_verified_comparison(root, *, loaded=None, source_roots=()):
    saved = read_json(local_file(root, "comparison.json"))
    try:
        sources = saved["source_runs"]
        if (
            type(saved["schema_version"]) is not int
            or type(saved["format_version"]) is not int
            or saved["schema_version"] != SCHEMA_VERSION
            or saved["format_version"] not in (1, 2, 3, 4)
            or len(sources) != 2
        ):
            raise EvidenceError("comparison_format_invalid")
        modern = saved["format_version"] == 4
        if source_roots and not modern:
            raise ContractError(
                "source_root", "legacy comparisons retain original absolute locators"
            )
        if modern:
            from inferyard.evidence.artifact_seal import read_sealed
            from inferyard.evidence.source_locations import resolve_source

            blobs = read_sealed(root, ["index.json", "report.html", "comparison.json"])
            if blobs["comparison.json"] != json_bytes(saved):
                raise EvidenceError("comparison_changed_during_read")
            paths = [resolve_source(root, ref["path"], source_roots) for ref in sources]
            loaded = (
                loaded
                if loaded is not None
                else [comparison_input(path, bind_unsealed=True) for path in paths]
            )
            actual = [{**ref, "path": sources[i]["path"]} for i, (_, ref) in enumerate(loaded)]
        else:
            paths = [Path(ref["path"]) for ref in sources]
            actual = [ref for _, ref in loaded] if loaded is not None else None
        if actual is not None and sources != actual:
            raise EvidenceError("report_comparison_sources_or_order_mismatch")
        options = {}
        if "performance_evidence" in saved:
            proofs = saved["performance_evidence"]
            if len(proofs) != 2:
                raise EvidenceError("comparison_format_invalid")
            options = {
                key: resolve_source(root, proof["source"]["path"], source_roots)
                if modern
                else Path(proof["source"]["path"])
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
        if modern:
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


def execute(request, *, options=None):
    if request.command == "compare-check":
        options = options or VerificationOptions.from_request(request)
        result = verify_comparison(request.run, source_roots=options.source_roots)
        return 0, CommandResult(request.command, "verified", "complete", details=result)
    loaded = [comparison_input(path, bind_unsealed=True) for path in (request.left, request.right)]
    result = build_comparison(
        request.left,
        request.right,
        request.comparison_mode,
        left_overhead=request.left_overhead,
        right_overhead=request.right_overhead,
        loaded=loaded,
        format_version=4,
    )
    from inferyard.reporting.report import build_index
    from inferyard.reporting.report_common import _environment, _new_output

    sources = [request.left, request.right]
    sources.extend(p for p in (request.left_overhead, request.right_overhead) if p is not None)
    _new_output(request.out, sources)
    from copy import deepcopy

    from inferyard.evidence.source_locations import relative_comparison
    from inferyard.reporting.sealed_report import portable_index, seal_report

    stored = deepcopy(result)
    relative_comparison(stored, request.out)
    atomic_bytes(request.out / "comparison.json", json_bytes(stored))
    index = build_index(
        [request.left, request.right],
        request.out,
        comparison_path=request.out,
        loaded=loaded,
        verified_comparison=stored,
        format_version=7,
    )
    index = portable_index(index, request.out, request.out)
    from inferyard.contracts.validation import strict_json_loads

    index = strict_json_loads(json_bytes(index).decode())
    html = _environment().get_template("report.html").render(index=index)
    atomic_bytes(request.out / "index.json", json_bytes(index))
    atomic_bytes(request.out / "report.html", html.encode())
    seal_report(request.out, comparison=True)
    return 0, CommandResult(
        request.command,
        "compared",
        "complete",
        evidence_dir=str(request.out),
        limitations=tuple(result["limitations"]),
        details=result,
    )
