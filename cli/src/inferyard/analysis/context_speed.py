"""Project verified trial values using the existing timing and token definitions."""

from collections import Counter
from statistics import median

from inferyard.analysis.context_speed_samples import sample
from inferyard.config.context_speed_profile import PROFILE
from inferyard.contracts.context_speed import (
    aggregate_reasons,
    sample_reasons,
    validate_result_bindings,
)
from inferyard.evidence.context_speed import reference


def value(number=None, reason="unknown", refs=()):
    return {
        "value": number,
        "missing_reason": reason if number is None else None,
        "evidence_refs": list(refs),
    }


def project(root, plan, loaded, runs):
    workload = plan["experiment"]["workloads"][0]
    config = loaded[workload["workload_id"]].config.to_dict()
    bundle = loaded[workload["workload_id"]].bundle.to_dict()
    context = config["conditions"]["context_size"]
    by_trial = {d["run"]["trial_id"]: d for d in runs}
    samples = []
    for trial in plan["trials"]:
        data = by_trial.get(trial["trial_id"])
        if data is not None:
            item = sample(root, trial, data)
        else:
            item = {
                "trial_id": trial["trial_id"],
                "repeat_index": trial["repeat_index"],
                "case_id": trial["case_order"][0],
                "run": None,
                "request_id": None,
                "execution_state": "not_executed",
                "error_category": None,
                "stop_reason": value(),
                "columns": {
                    k: value(reason="not_executed")
                    for k in ("context", "prompt_length", "time", "prefill", "decode")
                },
                "proof": {
                    "identity": [],
                    "token_budget": None,
                    "request": None,
                    "final_timing": None,
                    "fixed_output": [],
                },
                "measurement": {"valid": False, "reasons": [], "cohort_sha256": None},
            }
        reasons = list(
            dict.fromkeys([*item["measurement"]["reasons"], *sample_reasons(item, context)])
        )
        item["measurement"].update(valid=not reasons, reasons=reasons)
        samples.append(item)
    reasons = aggregate_reasons(samples)
    counts = Counter(s["execution_state"] for s in samples)
    summary = None
    if not reasons:
        summary = {"trial_ids": [s["trial_id"] for s in samples], "columns": {}}
        for key in ("context", "prompt_length"):
            summary["columns"][key] = value(
                samples[0]["columns"][key]["value"],
                refs=[r for s in samples for r in s["columns"][key]["evidence_refs"]],
            )
        for key in ("time", "prefill", "decode"):
            numbers = [s["columns"][key]["value"] for s in samples]
            summary["columns"][key] = {
                "median": median(numbers),
                "min": min(numbers),
                "max": max(numbers),
            }
    result = {
        "schema_version": 3,
        "kind": "context_speed_result.v1",
        "read_state": "readable",
        "profile": PROFILE,
        "source": {
            "batch_metadata": reference(root, "batch.json"),
            "plan": reference(root, "frozen-plan/plan.json"),
            "config": reference(
                root, "frozen-plan/" + plan["runtime_bindings"][0]["config"]["path"]
            ),
            "bundle": reference(root, "frozen-plan/" + workload["bundle"]["path"]),
            "plan_sha256": plan["plan_sha256"],
            "experiment_id": plan["experiment"]["experiment_id"],
            "workload_id": workload["workload_id"],
        },
        "metadata": {
            "model": {k: config["model"][k] for k in ("display_name", "sha256", "template_sha256")},
            "engine": {k: config["engine"][k] for k in ("adapter", "release", "binary_sha256")},
            "bundle": {k: bundle[k] for k in ("bundle_id", "version")},
            "selected_context_tokens": context,
        },
        "execution": {
            "counts": {
                "planned": 5,
                **{
                    k: counts[k]
                    for k in ("completed", "failed", "cancelled", "invalid", "not_executed")
                },
            },
            "stop_reason": next(
                (s["stop_reason"] for s in reversed(samples) if s["run"] is not None), value()
            ),
        },
        "measurement": {
            "status": "incomplete" if reasons else "complete",
            "valid_count": sum(s["measurement"]["valid"] for s in samples),
            "reasons": reasons,
        },
        "samples": samples,
        "summary": summary,
    }
    validate_result_bindings(result, plan, {d["run"]["run_id"]: d["run"] for d in runs})
    return result
