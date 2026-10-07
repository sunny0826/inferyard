"""Semantic invariants for sealing and fixed/duration summary accounting."""

from inferyard.contracts.validation import ContractError, ExecutionState, _rate_invariants


def _manifest(data):
    from inferyard.contracts.contracts_experiment import _reference

    for name, record in data["files"].items():
        if name == "manifest.json":
            raise ContractError("manifest.files", "manifest cannot contain itself")
        _reference({"path": name}, "manifest.files")
        expected = name in ("summary.json", "report.html", "requests.jsonl")
        if record["derived"] != expected:
            raise ContractError("manifest.files.derived", "artifact classification is code-owned")
    identity = ("kind", "execution_mode", "origin", "experiment_id", "trial_id")
    if any(key in data for key in identity) and not all(key in data for key in identity):
        raise ContractError("manifest", "run identity must be complete when supplied")


def _distribution(data, path):
    count = data["sample_count"]
    values = [data[key] for key in ("min", "p50", "max")]
    if count == 0:
        if any(value is not None for value in values):
            raise ContractError(path, "empty distribution must have null bounds")
    elif any(value is None for value in values) or not values[0] <= values[1] <= values[2]:
        raise ContractError(path, "invalid distribution bounds")
    if count < 20:
        if data["p95"] is not None or data["p95_reason"] != "insufficient_samples":
            raise ContractError(path, "p95 needs at least twenty observations")
    elif (
        data["p95"] is None
        or data["p95_reason"] is not None
        or not values[1] <= data["p95"] <= values[2]
    ):
        raise ContractError(path, "invalid p95 bounds")
    if data["p95_exploratory"] != (20 <= count < 100):
        raise ContractError(path, "inconsistent p95 qualification")


def _quality(summary):
    data, counts = summary["quality"], summary["counts"]
    complete = summary["completeness"] == "complete"
    total_valid = total_excluded = 0
    for category, group in data["Q01"].items():
        path = "summary.quality.Q01." + category
        _rate_invariants(group["rate"], path + ".rate")
        if group["passed"] + group["unscorable"] > group["valid"]:
            raise ContractError(path, "quality decisions exceed valid requests")
        if any(
            group["rate"][key] != group[field]
            for key, field in (
                ("numerator", "passed"),
                ("denominator", "valid"),
                ("excluded", "excluded"),
            )
        ):
            raise ContractError(path, "quality rate differs from group counts")
        if (not complete or group["unscorable"]) and group["rate"]["value"] is not None:
            raise ContractError(path, "incomplete quality must be null")
        if complete and group["unscorable"]:
            raise ContractError(path, "complete quality cannot retain unscorable requests")
        total_valid += group["valid"]
        total_excluded += group["excluded"]
    unscored = [summary["performance"].get(category, {}) for category in ("performance", "svg")]
    performance_valid = sum(
        group.get("completed", 0) + group.get("failed", 0) for group in unscored
    )
    performance_excluded = sum(group.get("planned", 0) for group in unscored) - performance_valid
    if (
        total_valid + performance_valid != counts["valid_executed"]
        or total_excluded + performance_excluded != counts["planned"] - counts["valid_executed"]
    ):
        raise ContractError(
            "summary.quality", "quality and performance counts must preserve ledger"
        )
    for code in ("Q02", "Q03", "Q04", "Q05", "Q06", "Q07"):
        _rate_invariants(data[code], "summary.quality." + code)
        if not complete and data[code]["value"] is not None:
            raise ContractError("summary.quality." + code, "incomplete quality must be null")
    classification = data["Q08"]
    if (classification["value"] is None) != (classification["reason"] is not None):
        raise ContractError("summary.quality.Q08", "missing classification needs a reason")
    if sum(item["count"] for item in classification["confusion"]) != classification["sample_count"]:
        raise ContractError("summary.quality.Q08", "confusion matrix differs from sample count")
    labels = [item["label"] for item in classification["classes"]]
    if len(labels) != len(set(labels)):
        raise ContractError("summary.quality.Q08", "duplicate classification label")


def _duration(data):
    counts, window = data["counts"], data["duration"]
    if counts["planned"] is not None or "request_limit" not in counts:
        raise ContractError("summary.counts", "duration requires a request limit and null planned")
    if counts["not_executed"] or counts["executed"] > counts["request_limit"]:
        raise ContractError("summary.counts", "duration ledger only contains admitted requests")
    if "Q01" in data["quality"]:
        raise ContractError("summary.quality", "duration probes are not independent quality cases")
    if window["reason"] == "window_not_started":
        if counts["executed"] or data["scope_complete"]:
            raise ContractError("summary.duration", "unstarted window cannot contain execution")
        return
    if (
        window["request_limit"] != counts["request_limit"]
        or window["admitted_requests"] != counts["executed"]
    ):
        raise ContractError("summary.duration", "admission count or request limit differs")
    if not window["start_ns"] < window["admission_deadline_ns"] < window["drain_deadline_ns"]:
        raise ContractError("summary.duration", "invalid window and drain ordering")
    if data["quality"]["independent_cases"] != window["independent_cases"]:
        raise ContractError("summary.duration", "probe denominator differs")
    if (window["reason"] == "window_close_missing") != ("closed_ns" not in window):
        raise ContractError("summary.duration", "window closure evidence differs from reason")
    if window["window_completed"]:
        if (
            window["reason"] != "duration_elapsed"
            or not window["admission_deadline_ns"]
            <= window["closed_ns"]
            <= window["drain_deadline_ns"]
        ):
            raise ContractError("summary.duration", "complete window requires timely drain")
        if counts["valid_executed"] != counts["executed"]:
            raise ContractError(
                "summary.duration", "complete window must drain all admitted requests"
            )
    if data["scope_complete"] and not (
        window["window_completed"] and window["probe_coverage_complete"]
    ):
        raise ContractError(
            "summary.duration", "complete scope requires full window and probe coverage"
        )
    started = 0
    cursor = window["start_ns"]
    probes = None
    for index, item in enumerate(window["windows"]):
        if item["index"] != index or item["start_ns"] != cursor or item["end_ns"] <= cursor:
            raise ContractError("summary.duration.windows", "noncontiguous or misordered windows")
        cursor = item["end_ns"]
        ids = [case["case_id"] for case in item["cases"]]
        if (
            len(ids) != window["independent_cases"]
            or len(set(ids)) != len(ids)
            or (probes is not None and probes != ids)
        ):
            raise ContractError("summary.duration.windows", "probe identities changed")
        probes = ids
        for case in item["cases"]:
            started += case["started"]
            states = case["execution_states"]
            if sum(states.values()) != case["started"] or case["valid_executed"] != states.get(
                "completed", 0
            ) + states.get("failed", 0):
                raise ContractError("summary.duration.windows", "probe cohort counts differ")
    if started != counts["executed"] or cursor != window["admission_deadline_ns"]:
        raise ContractError(
            "summary.duration.windows", "window coverage or admission total differs"
        )


def _summary(data):
    from inferyard.contracts.contracts_experiment import _observation

    counts = data["counts"]
    terminal_total = sum(counts[state.value] for state in ExecutionState)
    if counts["executed"] != terminal_total - counts["not_executed"]:
        raise ContractError("summary.counts.executed", "inconsistent executed count")
    valid = counts["completed"] + counts["failed"]
    if counts["valid_executed"] != valid:
        raise ContractError("summary.counts.valid_executed", "inconsistent valid count")
    if (
        not counts["budget_exhausted_completed"] <= counts["budget_exhausted"] <= valid
        or counts["budget_exhausted_completed"] > counts["completed"]
    ):
        raise ContractError("summary.counts", "budget exhaustion differs from valid ledger")
    if counts["budget_exhausted_other_diagnostic"] > counts["cancelled"] + counts["invalid"]:
        raise ContractError("summary.counts", "diagnostic exhaustion exceeds abnormal requests")
    rate = data["completion_rate"]
    _rate_invariants(rate, "summary.completion_rate")
    if (
        rate["numerator"] != counts["completed"]
        or rate["denominator"] != valid
        or rate["excluded"] != terminal_total - valid
    ):
        raise ContractError("summary.completion_rate", "completion rate differs from ledger")
    if data["completeness"] == "complete" and not (
        data["scope_complete"] and data["evidence_complete"]
    ):
        raise ContractError(
            "summary.completeness", "complete summary requires complete scope and evidence"
        )
    if data["scope_complete"] and (
        valid != terminal_total or data["stop_reason"] != "plan_finished"
    ):
        raise ContractError(
            "summary.scope_complete", "complete scope requires valid finalized execution"
        )
    if data["protocol_kind"] == "duration":
        if "duration" not in data or "idle_rss" not in data:
            raise ContractError("summary.duration", "duration evidence and idle sampling required")
        _duration(data)
    else:
        if (
            counts["planned"] != terminal_total
            or "request_limit" in counts
            or "duration" in data
            or "idle_rss" in data
        ):
            raise ContractError(
                "summary.counts.planned", "fixed plan must preserve five terminal counts"
            )
        if "Q01" not in data["quality"]:
            raise ContractError(
                "summary.quality", "fixed plan requires objective quality accounting"
            )
        _quality(data)
    for category, group in data["performance"].items():
        if (
            group["completed"] + group["failed"] > group["planned"]
            or group["timeout_count"] > group["failed"]
        ):
            raise ContractError("summary.performance." + category, "invalid request cohort")
        for code, metric in group["metrics"].items():
            if code == "L05":
                for request in metric:
                    _distribution(request["distribution"], "summary.performance.L05")
            else:
                _distribution(metric, "summary.performance." + code)
                if (
                    metric["sample_count"] > group["completed"]
                    or metric["sample_count"] + metric["excluded"] != group["planned"]
                ):
                    raise ContractError(
                        "summary.performance." + code, "latency denominator differs"
                    )
    if sum(group["planned"] for group in data["performance"].values()) != terminal_total:
        raise ContractError("summary.performance", "performance cohorts differ from ledger")
    for metric in data["metric_observations"]:
        _observation(metric)
        if metric["run_id"] != data["run_id"] or metric["trial_id"] != data["trial_id"]:
            raise ContractError(
                "summary.metric_observations", "metric identity differs from summary"
            )
        if data["completeness"] != "complete" and metric["comparison_eligible"]:
            raise ContractError(
                "summary.metric_observations", "incomplete trial cannot qualify metrics"
            )


def validate_storage(kind, data):
    (_manifest if kind == "manifest" else _summary)(data)
