"""Pure semantic validation; physical evidence verification belongs to the reader."""

import math
from collections import Counter
from statistics import median

from inferyard.analysis.engine_timing import capture_timings, engine_rates
from inferyard.config.context_speed_profile import CASES, PROFILE
from inferyard.contracts.validation import ContractError


def require(condition, reason):
    if not condition:
        raise ContractError("context_speed_result", reason)


def _value(value):
    require(
        (value["value"] is None) == (value["missing_reason"] is not None),
        "value_missing_reason_mismatch",
    )
    require(value["value"] is None or bool(value["evidence_refs"]), "value_evidence_missing")


def _refs(value):
    if isinstance(value, dict):
        if "path" in value and "sha256" in value:
            yield value
        else:
            for child in value.values():
                yield from _refs(child)
    elif isinstance(value, list):
        for child in value:
            yield from _refs(child)


def _contained(path):
    return (
        bool(path)
        and not path.startswith("/")
        and "\\" not in path
        and ":" not in path
        and all(p and not p.startswith(".") for p in path.split("/"))
    )


def _unique(values, reason):
    require(len(values) == len(set(values)), reason)


def sample_reasons(sample, selected_context):
    """Return deterministic protocol defects, independently of declared validity.

    References prove only shape/binding here. The producer must verify referenced
    bytes, identity, build_info, request scope and fixed-output evidence first.
    """
    reasons = []
    run, proof, columns = sample["run"], sample["proof"], sample["columns"]
    if sample["execution_state"] != "completed":
        reasons.append("request_not_completed")
    if run is None or run["integrity"] != "sealed":
        reasons.append("sealed_run_required")
    if sample["request_id"] is None:
        reasons.append("request_missing")
    if not proof["identity"]:
        reasons.append("identity_evidence_missing")
    if proof["token_budget"] is None:
        reasons.append("token_budget_evidence_missing")
    if proof["request"] is None:
        reasons.append("request_evidence_missing")
    if not proof["fixed_output"]:
        reasons.append("fixed_output_evidence_missing")
    if columns["context"]["value"] != selected_context:
        reasons.append("context_unverified")
    actual = columns["prompt_length"]["value"]
    if actual is None:
        reasons.append("actual_input_unknown")
    elif actual + 256 > selected_context:
        reasons.append("context_budget_exceeded")
    timing = proof["final_timing"]
    if timing is None:
        reasons.append("final_timing_missing")
    else:
        record = timing["record"]
        if not record["final"] or record["speculative"] or record["missing_reason"]:
            reasons.append("final_timing_unqualified")
        if record["cache_n"] != 0:
            reasons.append("cold_cache_unproven")
        if (
            actual is None
            or record["prompt_n"] is None
            or record["cache_n"] is None
            or (record["prompt_n"] + record["cache_n"] != actual)
        ):
            reasons.append("input_token_mismatch")
        if record["predicted_n"] != 256:
            reasons.append("fixed_output_not_reached")
    if any(columns[k]["value"] is None for k in ("time", "prefill", "decode")):
        reasons.append("metric_missing")
    return reasons


def aggregate_reasons(samples):
    """Summary eligibility; null cohorts never compare equal."""
    reasons = []
    if not all(s["measurement"]["valid"] for s in samples):
        reasons.append("five_valid_samples_required")
    cohorts = [s["measurement"]["cohort_sha256"] for s in samples]
    if any(c is None for c in cohorts):
        reasons.append("cohort_unknown")
    elif len(set(cohorts)) != 1:
        reasons.append("cohort_mismatch")
    for key in ("context", "prompt_length"):
        values = [s["columns"][key]["value"] for s in samples]
        if any(v is None for v in values) or len(set(values)) != 1:
            reasons.append("aggregate_" + key + "_mismatch")
    return reasons


def _sample(sample, context):
    for value in [sample["stop_reason"], *sample["columns"].values()]:
        _value(value)
    require(sample["case_id"] == CASES[context], "case_context_mismatch")
    run, proof, request = sample["run"], sample["proof"], sample["request_id"]
    if run is None:
        require(
            sample["execution_state"] == "not_executed" and request is None,
            "run_missing_for_execution",
        )
        require(not list(_refs(proof)), "proof_without_run")
        require(sample["measurement"]["cohort_sha256"] is None, "cohort_without_run")
        require(
            all(v["value"] is None for v in sample["columns"].values()), "observations_without_run"
        )
    else:
        require(_contained(run["path"]), "run_path_invalid")
        require(
            run["integrity"] != "sealed" or run["manifest_sha256"] is not None,
            "manifest_integrity_mismatch",
        )
        for ref in [run["inputs"], *list(_refs(proof)), *list(_refs(sample["columns"]))]:
            require(ref["path"].startswith(run["path"] + "/"), "run_reference_mismatch")
            member = ref.get("member", {})
            if member.get("kind") == "request":
                require(member["request_id"] == request, "request_reference_mismatch")
        if proof["request"] is not None:
            member = proof["request"].get("member", {})
            require(
                member.get("kind") == "request" and member.get("request_id") == request,
                "request_reference_mismatch",
            )
    timing = proof["final_timing"]
    if timing is not None:
        record = timing["record"]
        normalized = capture_timings(record, final=record["final"])
        normalized["speculative"] = record["speculative"]
        require(record == normalized, "timing_record_inconsistent")
        rates = engine_rates(
            {
                "engine_timings": [record],
                "engine_build_verified": True,
                "execution_state": sample["execution_state"],
            }
        )
        for column, code in (("prefill", "L06"), ("decode", "L07")):
            value = sample["columns"][column]["value"]
            if value is not None:
                require(
                    rates[code]["value"] is not None
                    and math.isclose(value, rates[code]["value"], rel_tol=1e-12),
                    "timing_value_mismatch",
                )
        value = sample["columns"]["time"]["value"]
        if value is not None:
            require(
                record["final"]
                and not record["speculative"]
                and record["prompt_ms"] is not None
                and record["prompt_ms"] > 0
                and math.isclose(value, record["prompt_ms"] / 1000, rel_tol=1e-12),
                "timing_value_mismatch",
            )
    else:
        require(
            all(sample["columns"][k]["value"] is None for k in ("time", "prefill", "decode")),
            "timing_evidence_missing",
        )
    defects = sample_reasons(sample, context)
    validity = sample["measurement"]
    _unique(validity["reasons"], "duplicate_measurement_reason")
    require(validity["valid"] == (not validity["reasons"]), "sample_validity_mismatch")
    require(set(defects).issubset(validity["reasons"]), "sample_defect_unreported")
    require(not validity["valid"] or not defects, "invalid_measurement_claim")


def validate_result(data):
    """Semantic hook after context_speed_result structural validation."""
    if data["read_state"] == "error":
        return
    for ref in _refs(data):
        require(_contained(ref["path"]), "evidence_path_invalid")
    _value(data["execution"]["stop_reason"])
    samples = data["samples"]
    require([s["repeat_index"] for s in samples] == list(range(5)), "repeat_order_mismatch")
    for key in ("trial_id", "request_id"):
        _unique([s[key] for s in samples if s[key] is not None], "duplicate_" + key)
    for key in ("run_id", "path"):
        _unique([s["run"][key] for s in samples if s["run"] is not None], "duplicate_run_" + key)
    for sample in samples:
        _sample(sample, data["metadata"]["selected_context_tokens"])
    counts = Counter(s["execution_state"] for s in samples)
    require(
        all(counts[k] == v for k, v in data["execution"]["counts"].items() if k != "planned"),
        "execution_count_mismatch",
    )
    if all(s["run"] is None for s in samples):
        stop = data["execution"]["stop_reason"]
        require(
            stop["value"] is None and stop["missing_reason"] == "unknown", "zero_run_stop_unknown"
        )
    measurement = data["measurement"]
    require(
        measurement["valid_count"] == sum(s["measurement"]["valid"] for s in samples),
        "valid_count_mismatch",
    )
    reasons = aggregate_reasons(samples)
    _unique(measurement["reasons"], "duplicate_measurement_reason")
    require(set(reasons).issubset(measurement["reasons"]), "aggregate_defect_unreported")
    complete = not measurement["reasons"]
    require((measurement["status"] == "complete") == complete, "measurement_status_mismatch")
    require((data["summary"] is not None) == complete, "summary_eligibility_mismatch")
    if complete:
        summary = data["summary"]
        require(summary["trial_ids"] == [s["trial_id"] for s in samples], "summary_trials_mismatch")
        for key in ("context", "prompt_length"):
            _value(summary["columns"][key])
            require(
                summary["columns"][key]["value"] == samples[0]["columns"][key]["value"],
                "summary_input_mismatch",
            )
        for key in ("time", "prefill", "decode"):
            values = [s["columns"][key]["value"] for s in samples]
            expected = {"median": median(values), "min": min(values), "max": max(values)}
            require(summary["columns"][key] == expected, "summary_statistics_mismatch")


def validate_result_bindings(data, plan, runs):
    """Bind a readable projection to already verified plan and run documents.

    runs maps run_id to the verified run document, never to directory names.
    No IO occurs; the caller owns hash/seal/request evidence authentication.
    """
    from inferyard.contracts.validation import validate_document

    validate_document("context_speed_result", data)
    require(data["read_state"] == "readable", "readable_result_required")
    validate_document("plan", plan)
    source = data["source"]
    require(plan["experiment"].get("profile") == PROFILE, "profile_mismatch")
    require(
        source["plan_sha256"] == plan["plan_sha256"]
        and source["experiment_id"] == plan["experiment"]["experiment_id"],
        "source_plan_mismatch",
    )
    workload = plan["experiment"]["workloads"][0]
    require(source["workload_id"] == workload["workload_id"], "source_workload_mismatch")
    require(len(plan["trials"]) == 5, "source_trials_mismatch")
    for sample, trial in zip(data["samples"], plan["trials"], strict=True):
        require(
            sample["trial_id"] == trial["trial_id"]
            and sample["repeat_index"] == trial["repeat_index"]
            and [sample["case_id"]] == trial["case_order"],
            "source_trial_mismatch",
        )
        if sample["run"] is not None:
            run = runs.get(sample["run"]["run_id"])
            require(run is not None, "source_run_missing")
            validate_document("run", run)
            require(
                run["run_id"] == sample["run"]["run_id"]
                and run["trial_id"] == sample["trial_id"]
                and run["plan_sha256"] == source["plan_sha256"]
                and run["experiment_id"] == source["experiment_id"]
                and run["origin"] == "measured"
                and run["kind"] == "run"
                and run["definition_versions"] == plan["experiment"]["definition_versions"],
                "source_run_mismatch",
            )


def validate_response(response, exit_code):
    """Validate the shared CommandResult envelope and its exit-code relationship."""
    from inferyard.contracts.validation import validate_document

    require(
        set(response)
        == {
            "command",
            "status",
            "completeness",
            "run_id",
            "evidence_dir",
            "limitations",
            "details",
        },
        "response_fields_mismatch",
    )
    require(
        response["command"] == "speed-results" and response["run_id"] is None,
        "response_identity_mismatch",
    )
    require(
        type(response["limitations"]) is list
        and all(type(v) is str for v in response["limitations"]),
        "response_limitations_invalid",
    )
    require(type(exit_code) is int, "exit_code_invalid")
    data = response["details"]
    validate_document("context_speed_result", data)
    if data["read_state"] == "error":
        require(
            exit_code in (2, 4)
            and response["status"] == "error"
            and response["completeness"] == "incomplete",
            "error_response_mismatch",
        )
        require(
            response["evidence_dir"] is None or type(response["evidence_dir"]) is str,
            "response_path_invalid",
        )
        return
    require(
        type(response["evidence_dir"]) is str and bool(response["evidence_dir"]),
        "response_path_invalid",
    )
    if exit_code == 0:
        require(
            response["status"] == "finished" and response["completeness"] == "complete",
            "complete_response_mismatch",
        )
        require(
            all(
                s["execution_state"] in ("completed", "failed")
                and s["run"] is not None
                and s["run"]["integrity"] == "sealed"
                for s in data["samples"]
            )
            and data["execution"]["stop_reason"]["value"] == "plan_finished",
            "execution_incomplete",
        )
    else:
        require(
            exit_code == 3
            and response["status"] == "partial"
            and response["completeness"] == "incomplete",
            "partial_response_mismatch",
        )
