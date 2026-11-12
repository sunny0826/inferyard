"""Position-variant quality summaries over frozen character spans."""

from collections import defaultdict

from inferyard.analysis.observations import Observations


def position_pattern(record):
    size = record["body_end"] - record["body_start"]
    fractions = [
        ((s["start"] + s["end"]) / 2 - record["body_start"]) / size
        for s in record["evidence_spans"]
    ]
    labels = ["front" if f < 1 / 3 else "middle" if f < 2 / 3 else "back" for f in fractions]
    return fractions, labels


def position_summary(run, workload, requests, counts, evidence, *, complete):
    if workload["purpose"] != "position":
        return None, []
    records = {r["case_id"]: r for r in workload["position_cases"]}
    output = Observations(run, workload["workload_id"], evidence, complete=complete)
    groups = defaultdict(list)
    variants = []
    for row in requests:
        record = records[row["case_id"]]
        actual = counts.get(row["case_id"], {}).get("actual_input_tokens")
        fractions, positions = position_pattern(record)
        groups[
            (record["family_id"], record["task_kind"], actual, tuple(positions), row["category"])
        ].append(row)
        variants.append(
            {
                "case_id": row["case_id"],
                "family_id": record["family_id"],
                "prompt_sha256": record["prompt_sha256"],
                "actual_input_tokens": actual,
                "position_fractions": fractions,
                "positions": positions,
            }
        )
    bins = []
    limits = [
        "variants_are_not_independent_questions",
        "position_is_character_midpoint_in_frozen_body",
        "no_long_context_comprehension_generalization",
        "comparison_gate_pending",
    ]
    for (family, task, actual, positions, category), rows in sorted(
        groups.items(), key=lambda item: str(item[0])
    ):
        valid = [r for r in rows if r["execution_state"] in ("completed", "failed")]
        passed = sum(
            r["execution_state"] == "completed"
            and (r.get("score") or {}).get("quality_state") == "pass"
            for r in valid
        )
        unscorable = any(
            r["execution_state"] == "completed"
            and (r.get("score") or {}).get("quality_state") not in ("pass", "fail")
            for r in valid
        )
        known = complete and actual is not None and bool(valid) and not unscorable
        value = passed / len(valid) if known else None
        bins.append(
            {
                "family_id": family,
                "task_kind": task,
                "actual_input_tokens": actual,
                "positions": list(positions),
                "category": category,
                "correct": passed,
                "valid_executed": len(valid),
                "excluded": len(rows) - len(valid),
                "value": value,
            }
        )
        output.add(
            "X02",
            "position_family_quality_rate",
            value,
            category=category,
            count=len(valid),
            numerator=passed if known else None,
            denominator=len(valid) if known else None,
            excluded=len(rows) - len(valid),
            reason="incomplete_unscorable_or_unknown_input_length",
            comparison=False,
            limits=limits,
            source=f"position_case_ledger:family={family}:kind={task}:tokens={actual}:positions={','.join(positions)}",
        )
    return {
        "family_count": len({r["family_id"] for r in records.values()}),
        "variant_count": len(records),
        "variants": variants,
        "bins": bins,
        "limitations": limits,
    }, output.items
