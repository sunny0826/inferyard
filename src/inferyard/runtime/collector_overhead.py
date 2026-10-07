"""Frozen ABBA collector preflight, with per-case paired workload latency.

This gate measures one declared workload and sampling interval only. It does not
make an environment comparable or establish a universal collector overhead.
"""

import hashlib
import math
from statistics import median

from inferyard import SCHEMA_VERSION
from inferyard.evidence.storage import EvidenceError, json_bytes
from inferyard.runtime.boundary_observer import boundary_contract
from inferyard.runtime.environment_observer import observer_contract

ORDER = ("off", "on", "on", "off")


def freeze_protocol(
    *,
    case_ids,
    interval_ms,
    tolerance_ratio,
    max_wall_seconds,
    config_sha256,
    bundle_sha256,
    tool_source_sha256,
    common_observer=False,
    boundary_observer=False,
    first_event_tolerance_ratio=None,
    engine_rate_tolerance_ratio=None,
    block_gap_tolerance_ms=None,
    resource_collector="linux-resource.v2",
):
    if resource_collector not in ("linux-resource.v2", "macos-resource.v1"):
        raise EvidenceError("unsupported_overhead_collector")
    if (
        not case_ids
        or len(set(case_ids)) != len(case_ids)
        or any(type(case) is not str or not case for case in case_ids)
    ):
        raise EvidenceError("overhead_requires_unique_frozen_cases")
    if type(interval_ms) is not int or interval_ms <= 0:
        raise EvidenceError("invalid_overhead_interval")
    if (
        type(tolerance_ratio) not in (int, float)
        or not math.isfinite(tolerance_ratio)
        or not 0 <= tolerance_ratio < 1
    ):
        raise EvidenceError("invalid_overhead_tolerance")
    if (
        type(max_wall_seconds) not in (int, float)
        or not math.isfinite(max_wall_seconds)
        or max_wall_seconds <= 0
    ):
        raise EvidenceError("invalid_overhead_wall_budget")
    hashes = (config_sha256, bundle_sha256, tool_source_sha256)
    if any(
        type(h) is not str or len(h) != 64 or any(c not in "0123456789abcdef" for c in h)
        for h in hashes
    ):
        raise EvidenceError("invalid_overhead_identity")
    protocol = {
        "schema_version": SCHEMA_VERSION,
        "kind": "collector_overhead_abba.v1",
        "order": list(ORDER),
        "case_ids": list(case_ids),
        "interval_ms": interval_ms,
        "tolerance_ratio": tolerance_ratio,
        "max_wall_seconds": max_wall_seconds,
        "config_sha256": config_sha256,
        "bundle_sha256": bundle_sha256,
        "tool_source_sha256": tool_source_sha256,
        "statistic": "max_absolute_adjacent_pair_case_median_relative_latency_change",
        "output_policy": "identical_content_reasoning_and_token_counts_per_case",
        "scope": "declared_workload_and_interval_only",
    }
    if resource_collector != "linux-resource.v2":
        protocol["resource_collector"] = resource_collector
    if (
        type(common_observer) is not bool
        or type(boundary_observer) is not bool
        or (common_observer and boundary_observer)
    ):
        raise EvidenceError("invalid_overhead_observer_mode")
    if common_observer:
        protocol.update(
            kind="collector_overhead_abba.v2",
            observer=observer_contract(interval_ms, collector=resource_collector),
            scope="incremental_resource_collection_with_common_environment_observer",
        )
    if boundary_observer:
        protocol.update(
            kind="collector_overhead_abba.v3",
            boundary_observer=boundary_contract(collector=resource_collector),
            scope="full_in_request_collection_with_shared_outside_request_guards",
        )
    if first_event_tolerance_ratio is not None:
        from inferyard.analysis.first_event_overhead import contract

        if not boundary_observer:
            raise EvidenceError("first_event_overhead_requires_boundary_observer")
        protocol["first_event_contract"] = contract(first_event_tolerance_ratio)
    if engine_rate_tolerance_ratio is not None:
        from inferyard.analysis.engine_overhead import contract

        if not boundary_observer:
            raise EvidenceError("engine_rate_overhead_requires_boundary_observer")
        protocol["engine_rate_contract"] = contract(engine_rate_tolerance_ratio)
    if block_gap_tolerance_ms is not None:
        from inferyard.analysis.block_overhead import contract

        if not boundary_observer:
            raise EvidenceError("block_gap_overhead_requires_boundary_observer")
        protocol["block_gap_contract"] = contract(block_gap_tolerance_ms)
    return {**protocol, "protocol_sha256": hashlib.sha256(json_bytes(protocol)).hexdigest()}


def validate_protocol(protocol):
    keys = (
        "case_ids",
        "interval_ms",
        "tolerance_ratio",
        "max_wall_seconds",
        "config_sha256",
        "bundle_sha256",
        "tool_source_sha256",
    )
    try:
        expected = freeze_protocol(
            **{key: protocol[key] for key in keys},
            resource_collector=protocol.get("resource_collector", "linux-resource.v2"),
            common_observer=protocol.get("kind") == "collector_overhead_abba.v2",
            boundary_observer=protocol.get("kind") == "collector_overhead_abba.v3",
            block_gap_tolerance_ms=(protocol.get("block_gap_contract") or {}).get("tolerance_ms"),
            engine_rate_tolerance_ratio=(protocol.get("engine_rate_contract") or {}).get(
                "tolerance_ratio"
            ),
            first_event_tolerance_ratio=(protocol.get("first_event_contract") or {}).get(
                "tolerance_ratio"
            ),
        )
    except (KeyError, TypeError, AttributeError) as exc:
        raise EvidenceError("invalid_overhead_protocol") from exc
    if protocol != expected:
        raise EvidenceError("overhead_protocol_mismatch")


def assess_overhead(protocol, trials):
    validate_protocol(protocol)
    result = {
        "protocol_sha256": protocol["protocol_sha256"],
        "status": "incomplete",
        "passed": False,
        "pairs": [],
        "reasons": [],
        "tolerance_ratio": protocol["tolerance_ratio"],
        "limitations": [
            "four_runs_not_population_confidence",
            "declared_workload_and_interval_only",
            "environment_comparability_is_a_separate_gate",
        ],
    }
    if protocol["kind"] == "collector_overhead_abba.v2":
        result["measurement_scope"] = protocol["scope"]
        result["limitations"].append("common_environment_observer_perturbation_not_measured")
    if protocol["kind"] == "collector_overhead_abba.v3":
        result["measurement_scope"] = protocol["scope"]
        result["limitations"].extend(
            [
                "outside_request_guards_can_affect_cache_and_spacing",
                "baseline_environment_is_bracketed_not_continuously_observed",
                "outside_request_guard_cost_not_part_of_latency_perturbation",
            ]
        )
    if len(trials) != 4:
        result["reasons"].append("four_completed_runs_required")
        return result
    for mode, trial in zip(ORDER, trials, strict=True):
        if trial.get("mode") != mode or trial.get("protocol_sha256") != protocol["protocol_sha256"]:
            raise EvidenceError("overhead_trial_binding_mismatch")
        if trial.get("completed") is not True:
            result["reasons"].append("overhead_run_not_finalized")
        rows = trial.get("requests", [])
        if [r.get("case_id") for r in rows] != protocol["case_ids"]:
            result["reasons"].append("overhead_case_order_mismatch")
        for row in rows:
            duration = row.get("duration_ns")
            if (
                row.get("execution_state") != "completed"
                or type(duration) is not int
                or duration <= 0
                or not row.get("output_identity")
            ):
                result["reasons"].append("overhead_request_invalid_or_incomplete")
    if result["reasons"]:
        result["reasons"] = sorted(set(result["reasons"]))
        return result
    if any(
        len({trial["requests"][i]["output_identity"] for trial in trials}) != 1
        for i in range(len(protocol["case_ids"]))
    ):
        result["reasons"].append("output_work_differs")
        return result
    for off, on in ((0, 1), (3, 2)):
        changes = [
            (b["duration_ns"] - a["duration_ns"]) / a["duration_ns"]
            for a, b in zip(trials[off]["requests"], trials[on]["requests"], strict=True)
        ]
        result["pairs"].append(
            {
                "off_run": off + 1,
                "on_run": on + 1,
                "case_relative_latency_changes": changes,
                "median_relative_latency_change": median(changes),
            }
        )
    # Positive and negative distortions both fail: a faster run can indicate cache
    # or drift. Opposite signs must not cancel into an apparently negligible effect.
    worst = max(abs(pair["median_relative_latency_change"]) for pair in result["pairs"])
    result.update(
        status="evaluated",
        passed=worst <= protocol["tolerance_ratio"],
        max_absolute_pair_median_change=worst,
    )
    if not result["passed"]:
        result["reasons"].append("collector_perturbation_exceeds_frozen_tolerance")
    if "first_event_contract" in protocol:
        from inferyard.analysis.first_event_overhead import assess

        result["first_event_assessments"] = assess(protocol, trials)
    if "engine_rate_contract" in protocol:
        from inferyard.analysis.engine_overhead import assess

        result["engine_rate_assessments"] = assess(protocol, trials)
    if "block_gap_contract" in protocol:
        from inferyard.analysis.block_overhead import assess

        result["block_gap_assessments"] = assess(protocol, trials)
    return result
