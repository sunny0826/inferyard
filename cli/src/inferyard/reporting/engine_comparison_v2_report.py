"""Public engine-comparison.v2 derivation, report and sealed verification."""

from __future__ import annotations

import hashlib
from importlib.resources import files
from pathlib import Path

from inferyard.analysis.comparison_types import JSONRecord, JSONValue
from inferyard.analysis.engine_comparison_v2 import (
    DEFINITION,
    bundle_comparisons,
    derive_comparison_v2,
)
from inferyard.application.types import CommandRequest, CommandResult, VerificationOptions
from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.evidence.artifact_seal import read_sealed, seal
from inferyard.evidence.source_locations import relative_source, resolve_source
from inferyard.evidence.storage import EvidenceError, atomic_bytes, json_bytes
from inferyard.reporting.report_common import _new_output

RESULT = "engine-comparison.json"
HTML = "report.html"
TEMPLATE = "engine_comparison_v2.html"
SEALED = (RESULT, HTML)
V1_DEFINITION = "engine-comparison.v1"


def template_sha256() -> str:
    raw = files("inferyard").joinpath("templates", TEMPLATE).read_bytes()
    return hashlib.sha256(raw).hexdigest()


def _source_data(
    document: JSONRecord, runs: list[JSONRecord], out: Path
) -> tuple[dict[str, JSONRecord], list[JSONRecord]]:
    from inferyard.analysis.first_body import attach_first_body
    from inferyard.reporting.comparison_report import comparison_input

    by_run_id = {entry["run"]["run_id"]: entry for entry in runs}
    data, refs = {}, []
    for row in document["handoff"]["slots"]:
        run_id = row.get("run_id")
        if run_id is None or run_id not in by_run_id:
            continue
        metadata = {}
        trial, reference = comparison_input(
            by_run_id[run_id]["path"], bind_unsealed=True, metadata=metadata
        )
        attach_first_body(trial, metadata["evidence"].jsonl("events.jsonl")[0])
        data[run_id] = trial
        refs.append(
            {
                "trial_id": row["trial_id"],
                **reference,
                "path": relative_source(reference["path"], out),
            }
        )
    return data, refs


def _empty_metrics(comparisons: JSONRecord) -> list[JSONRecord]:
    rows = []
    for bundle, result in comparisons.items():
        if result.get("eligible") is not False:
            continue
        blockers = result.get("blockers") or ["comparison_overall_blocked"]
        for metric, row in result.get("verdicts", {}).items():
            if row.get("verdict") == "not_eligible":
                rows.append(
                    {
                        "bundle": bundle,
                        "metric": metric,
                        "value": None,
                        "reason": blockers[0],
                    }
                )
    return rows


def _limitations(comparisons: JSONRecord, eligible: bool) -> list[str]:
    values = {
        item
        for result in comparisons.values()
        for item in result.get("limitations", [])
    }
    if not eligible:
        values.add("comparison_output_blocked_no_metrics")
    return sorted(values)


def _comparison_state(
    document: JSONRecord, data: dict[str, JSONRecord]
) -> tuple[JSONRecord, bool, set[str]]:
    comparisons = (
        bundle_comparisons(document, data) if document["bundles"] else {}
    )
    blockers = {
        blocker
        for comparison in comparisons.values()
        for blocker in comparison.get("blockers", [])
    }
    eligible = (
        bool(comparisons)
        and document["eligible"]
        and all(comparison["eligible"] for comparison in comparisons.values())
    )
    return comparisons, eligible, blockers


def build(
    plan_path: Path,
    batch_root: Path,
    out: Path,
    *,
    producer: JSONRecord | None = None,
) -> JSONRecord:
    """Derive v2 from one frozen plan and one complete batch without measuring."""
    from inferyard.provenance import tool_source_hash

    out = out.resolve()
    plan_path = plan_path.resolve()
    batch_root = batch_root.resolve()
    document, runs = derive_comparison_v2(plan_path, batch_root)
    data, source_runs = _source_data(document, runs, out)
    comparisons, eligible, comparison_blockers = _comparison_state(document, data)
    return {
        "definition": DEFINITION,
        "plan": {
            "path": relative_source(plan_path, out),
            "sha256": document["plan"]["sha256"],
        },
        "batch": {
            "path": relative_source(batch_root, out),
            "plan_sha256": document["plan"]["sha256"],
        },
        "run_set": document["run_set"],
        "handoff": document["handoff"],
        "history_stop": document["history_stop"],
        "bundles": document["bundles"],
        "comparisons": comparisons,
        "source_runs": source_runs,
        "diagnostics": document["diagnostics"],
        "eligible": eligible,
        "blockers": sorted(
            {row["reason"] for row in document["diagnostics"]} | comparison_blockers
        ),
        "empty_metrics": _empty_metrics(comparisons),
        "limitations": _limitations(comparisons, eligible),
        "producer": producer
        or {
            "tool_source_sha256": tool_source_hash(),
            "template_sha256": template_sha256(),
        },
    }


def render(result: JSONRecord) -> str:
    from inferyard.reporting.report_common import _environment

    return _environment().get_template(TEMPLATE).render(result=result)


def _hash(value: JSONValue) -> bool:
    return type(value) is str and len(value) == 64 and all(
        character in "0123456789abcdef" for character in value
    )


def _metadata_valid(saved: JSONRecord) -> bool:
    if (
        type(saved.get("plan")) is not dict
        or type(saved.get("batch")) is not dict
        or type(saved.get("source_runs")) is not list
        or type(saved.get("comparisons")) is not dict
    ):
        return False
    return all(
        type(ref.get("path")) is str and bool(ref["path"])
        for ref in (saved["plan"], saved["batch"])
    )


def _producer_valid(saved: JSONRecord) -> bool:
    producer = saved.get("producer")
    return (
        type(producer) is dict
        and set(producer) == {"tool_source_sha256", "template_sha256"}
        and all(_hash(value) for value in producer.values())
    )


def _saved(raw: bytes) -> JSONRecord:
    try:
        saved = strict_json_loads(raw.decode("utf-8"))
    except (UnicodeError, ContractError) as exc:
        raise EvidenceError("engine_comparison_format_invalid") from exc
    if (
        type(saved) is not dict
        or saved.get("definition") != DEFINITION
        or not _metadata_valid(saved)
        or not _producer_valid(saved)
        or any(
            type(row) is not dict or type(row.get("run_id")) is not str
            for row in saved["source_runs"]
        )
    ):
        raise EvidenceError("engine_comparison_format_invalid")
    return saved


def _restore_paths(expected: JSONRecord, saved: JSONRecord) -> None:
    """Keep saved path strings when only the source root moved."""
    for key, value in expected.items():
        if key not in saved:
            continue
        stored = saved[key]
        if key == "path" and type(value) is str and type(stored) is str:
            expected[key] = stored
        elif type(value) is dict and type(stored) is dict:
            _restore_paths(value, stored)
        elif type(value) is list and type(stored) is list and len(value) == len(stored):
            for item, original in zip(value, stored, strict=True):
                if type(item) is dict and type(original) is dict:
                    _restore_paths(item, original)


def _restore_source_paths(expected: JSONRecord, saved: JSONRecord) -> None:
    _restore_paths(expected, saved)
    for ref, stored in zip(
        expected["source_runs"], saved["source_runs"], strict=False
    ):
        if ref.get("run_id") != stored.get("run_id"):
            raise EvidenceError("engine_comparison_sources_mismatch")


def _expected_artifact(
    root: Path, saved: JSONRecord, options: VerificationOptions
) -> JSONRecord:
    plan_path = resolve_source(root, saved["plan"]["path"], options.source_roots)
    batch_path = resolve_source(root, saved["batch"]["path"], options.source_roots)
    expected = build(plan_path, batch_path, root, producer=saved["producer"])
    _restore_source_paths(expected, saved)
    return expected


def verify(root: Path, *, options: VerificationOptions) -> JSONRecord:
    """Verify a sealed v2 artifact by rebuilding it from the referenced plan/batch."""
    from inferyard.reporting.engine_blocks_verify import verify as verify_v1

    blobs = read_sealed(
        root,
        SEALED,
        retain={RESULT, *([HTML] if options.rerender else [])},
    )
    try:
        marker = strict_json_loads(blobs[RESULT].decode("utf-8"))
    except (UnicodeError, ContractError) as exc:
        raise EvidenceError("engine_comparison_format_invalid") from exc
    if type(marker) is dict and marker.get("definition") == V1_DEFINITION:
        return verify_v1(root, options=options)
    saved = _saved(blobs[RESULT])
    if json_bytes(_expected_artifact(root, saved, options)) != json_bytes(saved):
        raise EvidenceError("engine_comparison_recomputation_mismatch")
    if options.rerender and render(saved).encode() != blobs[HTML]:
        raise EvidenceError("engine_comparison_html_recomputation_mismatch")
    return {
        "verified": True,
        "source_runs": len(saved["source_runs"]),
        "bytes_verified": True,
        "sources_verified": True,
        "semantic_verified": True,
        "render_checked": options.rerender,
    }


def _derivation_targets(request: CommandRequest) -> tuple[Path, Path, Path]:
    if request.frozen_plan is None or request.batch is None or request.out is None:
        raise ContractError(
            "compare_engines.input", "compare-engines requires --plan, --batch and --out"
        )
    return request.frozen_plan, request.batch, request.out


def _publish(request: CommandRequest, result: JSONRecord) -> None:
    plan_path, batch_root, out = _derivation_targets(request)
    _new_output(out, [plan_path.parent if plan_path.is_file() else plan_path, batch_root])
    raw = json_bytes(result)
    atomic_bytes(out / RESULT, raw)
    # Render the saved form so verify --rerender sees the same key order.
    atomic_bytes(out / HTML, render(strict_json_loads(raw.decode())).encode())
    seal(out, SEALED)


def execute(
    request: CommandRequest, *, options: VerificationOptions | None = None
) -> tuple[int, CommandResult]:
    if request.command == "engine-compare-check":
        options = options or VerificationOptions.from_request(request)
        details = verify(request.run, options=options)
        return 0, CommandResult(request.command, "verified", "complete", details=details)
    plan_path, batch_root, out = _derivation_targets(request)
    result = build(plan_path, batch_root, out)
    _publish(request, result)
    return 0, CommandResult(
        request.command,
        "derived",
        "complete",
        evidence_dir=str(out),
        limitations=tuple(result["limitations"]),
        details=result,
    )
