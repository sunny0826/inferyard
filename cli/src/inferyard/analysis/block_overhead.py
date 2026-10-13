"""Request-weighted L05 qualification; decoded chunks are never token samples."""

import hashlib
import math
from statistics import median

from inferyard.analysis.performance import distribution
from inferyard.evidence.storage import EvidenceError, json_bytes

DEFINITION = "block_gap_abba.v1"
STATISTICS = ("min", "p50", "p95", "max")


def contract(tolerance_ms):
    if (
        type(tolerance_ms) not in (int, float)
        or not math.isfinite(tolerance_ms)
        or tolerance_ms < 0
    ):
        raise EvidenceError("invalid_block_gap_overhead_tolerance")
    return {
        "definition": DEFINITION,
        "metric_id": "L05",
        "statistics": ["within_request_" + s for s in STATISTICS],
        "tolerance_ms": tolerance_ms,
        "statistic": "max_absolute_adjacent_pair_case_median_gap_change_ms",
        "shape_policy": "equal_decoded_delta_channel_sequence_per_case",
        "missing_policy": "every_case_every_arm_finite_nonnegative_statistic_p95_requires_20_gaps",
        "quantile_method": "nearest_rank.phase2.v1",
        "scope": "client_decoded_chunks_not_token_itl_or_exact_text_boundary_identity",
    }


def projection(row):
    capture = row.get("arrival_capture")
    blocks = row.get("block_arrivals", [])
    start, end = row.get("t_send_ns"), row.get("t_terminal_ns")
    valid = (
        row.get("execution_state") == "completed"
        and capture == {"streaming": True, "source": "decoded_delta"}
        and type(start) is int
        and type(end) is int
        and 0 <= start <= end
        and len(blocks) >= 2
    )
    last = start
    for index, block in enumerate(blocks, 1):
        stamp = block.get("monotonic_ns")
        valid = valid and (
            type(block.get("index")) is int
            and block["index"] == index
            and block.get("channel") in ("content", "reasoning")
            and type(stamp) is int
            and last <= stamp <= end
        )
        if not valid:
            break
        last = stamp
    gaps = (
        [
            (b["monotonic_ns"] - a["monotonic_ns"]) / 1e6
            for a, b in zip(blocks, blocks[1:], strict=False)
        ]
        if valid
        else []
    )
    stats = distribution(gaps)
    return {
        "shape": hashlib.sha256(
            json_bytes(
                {
                    "capture": capture,
                    "channels": [b["channel"] for b in blocks],
                }
            )
        ).hexdigest()
        if valid
        else None,
        "gap_count": len(gaps),
        "statistics": {"within_request_" + s: stats[s] for s in STATISTICS},
    }


def known(row, statistic):
    value = row.get("statistics", {}).get(statistic)
    count = row.get("gap_count")
    return (
        type(value) in (int, float)
        and math.isfinite(value)
        and value >= 0
        and type(count) is int
        and count >= (20 if statistic == "within_request_p95" else 1)
        and isinstance(row.get("shape"), str)
        and len(row["shape"]) == 64
    )


def assess(protocol, trials):
    frozen = protocol["block_gap_contract"]
    rows = [[r.get("block_gaps", {}) for r in t["requests"]] for t in trials]
    results = {}
    for statistic in frozen["statistics"]:
        valid = len(rows) == 4 and all(
            len(arm) == len(protocol["case_ids"]) and all(known(r, statistic) for r in arm)
            for arm in rows
        )
        matched = valid and all(
            len({(arm[i]["shape"], arm[i]["gap_count"]) for arm in rows}) == 1
            for i in range(len(protocol["case_ids"]))
        )
        pairs = []
        if matched:
            for off, on in ((0, 1), (3, 2)):
                changes = [
                    b["statistics"][statistic] - a["statistics"][statistic]
                    for a, b in zip(rows[off], rows[on], strict=True)
                ]
                pairs.append(
                    {
                        "off_run": off + 1,
                        "on_run": on + 1,
                        "case_gap_changes_ms": changes,
                        "median_gap_change_ms": median(changes),
                    }
                )
        worst = max((abs(p["median_gap_change_ms"]) for p in pairs), default=None)
        passed = matched and worst <= frozen["tolerance_ms"]
        results[statistic] = {
            "definition": DEFINITION,
            "passed": passed,
            "tolerance_ms": frozen["tolerance_ms"],
            "pairs": pairs,
            "max_absolute_pair_median_change_ms": worst,
            "reasons": []
            if passed
            else [
                "block_statistic_missing_or_invalid"
                if not valid
                else "block_shape_differs"
                if not matched
                else "block_gap_perturbation_exceeds_frozen_tolerance"
            ],
        }
    return results


def pair_reasons(statistic, assessments, cohorts):
    reasons = []
    if any(a.get("passed") is not True for a in assessments):
        reasons.append("block_gap_overhead_not_qualified_on_both_sides")
    if (
        any(a.get("definition") != DEFINITION for a in assessments)
        or assessments[0].get("tolerance_ms") is None
        or assessments[0].get("tolerance_ms") != assessments[1].get("tolerance_ms")
    ):
        reasons.append("block_gap_overhead_contract_mismatch")
    if any(len(rows) != 1 for rows in cohorts):
        return reasons + ["block_gaps_require_one_request_per_case"]
    a, b = [projection(rows[0]) for rows in cohorts]
    if (
        not known(a, statistic)
        or not known(b, statistic)
        or a["shape"] != b["shape"]
        or a["gap_count"] != b["gap_count"]
    ):
        reasons.append("block_target_shape_or_statistic_unknown_or_different")
    return reasons
