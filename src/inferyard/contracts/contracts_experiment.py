"""Semantic validation of experiments and evidence envelopes."""

import math
from pathlib import PurePosixPath, PureWindowsPath

from inferyard.config.plan_math import plan_hash, workload_budget
from inferyard.contracts.validation import ContractError


def unique(values, path):
    if len(values) != len(set(values)):
        raise ContractError(path, "duplicate identifier")


def _reference(ref, path):
    name = PurePosixPath(ref["path"])
    if (
        name.is_absolute()
        or PureWindowsPath(ref["path"]).drive
        or ".." in name.parts
        or any(character in ref["path"] for character in "\\:")
    ):
        raise ContractError(path, "requires a relative contained artifact path")
    if not name.parts or any(part.startswith(".") for part in name.parts):
        raise ContractError(path, "invalid artifact path")


def _environment_admission(data, path):
    if "environment_admission" in data:
        unique(data["environment_admission"]["required_fields"], path + ".required_fields")


def _experiment(data):
    _environment_admission(data, "experiment.environment_admission")
    execution = data["execution"]
    if (execution["order"] == "seeded") != (execution["seed"] is not None):
        raise ContractError("experiment.execution.seed", "required exactly for seeded order")
    comparison = data["comparison"]
    if (comparison["mode"] == "config") != (comparison["factor"] is not None):
        raise ContractError("experiment.comparison.factor", "required exactly for config mode")
    unique([w["workload_id"] for w in data["workloads"]], "experiment.workloads")
    for i, workload in enumerate(data["workloads"]):
        path = f"experiment.workloads[{i}]"
        for field in ("config", "bundle"):
            _reference(workload[field], f"{path}.{field}.path")
        tolerance = workload.get("input_tolerance_tokens", 0)
        target = workload["input_target_tokens"]
        if (target is None and tolerance) or (target is not None and tolerance >= target):
            raise ContractError(
                path + ".input_tolerance_tokens", "requires target and a positive lower bound"
            )
        protocol = workload["protocol"]
        if "output_mode" in workload and (
            workload["purpose"] != "performance" or protocol["kind"] != "fixed"
        ):
            raise ContractError(path + ".output_mode", "requires fixed performance workload")
        unique(protocol["case_ids"], path + ".protocol.case_ids")
        if protocol["kind"] == "duration":
            if any(
                protocol[k] < 1e-9
                for k in ("duration_seconds", "window_seconds", "drain_timeout_seconds")
            ):
                raise ContractError(path + ".protocol", "duration below clock resolution")
            if protocol["duration_seconds"] / protocol["window_seconds"] > 100_000:
                raise ContractError(path + ".protocol", "window expansion limit exceeded")
            if workload["purpose"] != "stability":
                raise ContractError(path + ".purpose", "duration requires stability purpose")
            if protocol["window_seconds"] > protocol["duration_seconds"]:
                raise ContractError(path + ".protocol.window_seconds", "exceeds duration")
            if protocol["drain_timeout_seconds"] < workload["timeout_seconds"]:
                raise ContractError(path + ".protocol.drain_timeout_seconds", "below deadline")
        elif workload["purpose"] == "stability":
            raise ContractError(path + ".protocol.kind", "stability requires duration protocol")


def _plan(data):
    _experiment(data["experiment"])
    unique([t["trial_id"] for t in data["trials"]], "plan.trials")
    workloads = {w["workload_id"]: w for w in data["experiment"]["workloads"]}
    bindings = data["runtime_bindings"]
    unique([b["workload_id"] for b in bindings], "plan.runtime_bindings")
    if bindings and {b["workload_id"] for b in bindings} != set(workloads):
        raise ContractError("plan.runtime_bindings", "bindings must cover every workload")
    for binding in bindings:
        _reference(binding["config"], "plan.runtime_bindings.config")
        origin = binding["source_directory"]
        if not (PurePosixPath(origin).is_absolute() or PureWindowsPath(origin).is_absolute()):
            raise ContractError(
                "plan.runtime_bindings.source_directory", "requires absolute origin"
            )
    repeats = {key: [] for key in workloads}
    for i, trial in enumerate(data["trials"]):
        path = f"plan.trials[{i}]"
        workload = workloads.get(trial["workload_id"])
        if workload is None:
            raise ContractError(path + ".workload_id", "unknown workload")
        repeats[trial["workload_id"]].append(trial["repeat_index"])
        protocol = workload["protocol"]
        unique(trial["case_order"], path + ".case_order")
        if set(trial["case_order"]) != set(protocol["case_ids"]):
            raise ContractError(path + ".case_order", "does not match frozen case set")
        if data["experiment"]["execution"]["order"] == "fixed":
            if trial["case_order"] != protocol["case_ids"]:
                raise ContractError(path + ".case_order", "fixed order changed")
        limit, expected, total = workload_budget(workload)
        if trial["request_limit"] != limit:
            raise ContractError(path + ".request_limit", "inconsistent request limit")
        if not math.isclose(trial["request_budget_seconds"], expected):
            raise ContractError(path + ".request_budget_seconds", "inconsistent budget")
        if not math.isclose(trial["total_budget_seconds"], total):
            raise ContractError(path + ".total_budget_seconds", "inconsistent total budget")
    for key, indices in repeats.items():
        if len(indices) != workloads[key]["repeats"] or sorted(indices) != list(
            range(len(indices))
        ):
            raise ContractError("plan.trials", "missing or duplicate repetition")
    limit = sum(t["request_limit"] for t in data["trials"])
    seconds = sum(t["request_budget_seconds"] for t in data["trials"])
    total_seconds = sum(t["total_budget_seconds"] for t in data["trials"])
    if data["request_limit"] != limit or not math.isclose(data["request_budget_seconds"], seconds):
        raise ContractError("plan", "totals do not match trials")
    if not math.isclose(data["total_budget_seconds"], total_seconds):
        raise ContractError("plan.total_budget_seconds", "totals do not match trials")
    budget = data["experiment"]["budget"]
    if limit > budget["max_requests"] or data["total_budget_seconds"] > budget["max_wall_seconds"]:
        raise ContractError("plan", "exceeds frozen budget")
    if data["plan_sha256"] != plan_hash(data):
        raise ContractError("plan.plan_sha256", "content hash mismatch")


def _run(data):
    if "implementation_identity" in data:
        from inferyard.implementation_identity import validate_identity

        validate_identity(data["implementation_identity"])
    if (data["relation"] != "initial") != (data["parent_run_id"] is not None):
        raise ContractError("run.parent_run_id", "lineage relation requires a parent")
    if data["parent_run_id"] == data["run_id"]:
        raise ContractError("run.parent_run_id", "cannot reference self")
    if bool(data["resumed_case_ids"]) != (data["relation"] == "resume"):
        raise ContractError("run.resumed_case_ids", "required exactly for resume")
    unique(data["resumed_case_ids"], "run.resumed_case_ids")


def _observation(data):
    if "analysis_id" in data:
        unique(data["source_run_ids"], "metric_observation.source_run_ids")
    absent = data["status"] in ("missing", "not_applicable")
    if absent != (data["value"] is None) or absent != (data["missing_reason"] is not None):
        raise ContractError("metric_observation.status", "value and missing reason disagree")
    if absent and data["comparison_eligible"]:
        raise ContractError(
            "metric_observation.comparison_eligible", "missing values not comparable"
        )
    if not absent and (not data["sample_count"] or not data["evidence_refs"]):
        raise ContractError("metric_observation", "observed value requires samples and evidence")
    start, end = data["sampled_start_ns"], data["sampled_end_ns"]
    if (start is None) != (end is None) or (start is not None and end < start):
        raise ContractError("metric_observation.sampled_end_ns", "invalid observation interval")
    if data["coverage_ratio"] is not None and start is None:
        raise ContractError("metric_observation.coverage_ratio", "requires an observation interval")
    num, den = data["numerator"], data["denominator"]
    if (num is None) != (den is None):
        raise ContractError("metric_observation.denominator", "rate requires both counts")
    if num is not None:
        if num > den or (den == 0 and data["value"] is not None):
            raise ContractError("metric_observation.numerator", "invalid rate counts")
        if data["value"] is not None and not math.isclose(data["value"], num / den):
            raise ContractError("metric_observation.value", "inconsistent rate")
    for ref in data["evidence_refs"]:
        _reference(ref, "metric_observation.evidence_refs")


def validate_semantics(kind, data):
    if kind in ("config", "config_input"):
        from inferyard.contracts.lab import validate_config

        validate_config(data)
        _environment_admission(data["conditions"], "config.conditions.environment_admission")
    if kind == "config" and "macos_power_policy" in data["conditions"]:
        from inferyard.platforms.power_macos import valid

        if not valid(data["conditions"]["macos_power_policy"]):
            raise ContractError(
                "config.conditions.macos_power_policy", "inconsistent native policy"
            )
        return
    if kind in ("event", "sample"):
        from inferyard.contracts.validation import _event_invariants, _sample_invariants

        if kind == "sample":
            _sample_invariants(data)
            if data.get("collector") in ("linux-resource.v2", "macos-resource.v1"):
                from inferyard.contracts.schemas_resources import validate_resource_sample

                validate_resource_sample(data)
            elif data.get("collector") in ("linux-sensors.v2", "macos-smc.v1"):
                from inferyard.contracts.schemas_sensors import validate_sensor_sample

                validate_sensor_sample(data)
        else:
            if data["event_type"] in ("duration_started", "duration_closed"):
                if data["request_id"] is not None or data["phase"] != "formal":
                    raise ContractError("event.duration", "window event must not bind a request")
                return
            _event_invariants(data)
            if data["event_type"] == "score":
                validate_semantics("score", data["data"])
            elif data["event_type"] == "engine_timings":
                from inferyard.analysis.engine_timing import capture_timings

                raw = data["data"]
                expected = capture_timings(raw, final=raw["final"])
                expected["speculative"] = raw["speculative"]
                if raw != expected:
                    raise ContractError(
                        "event.engine_timings", "inconsistent numeric timing evidence"
                    )
        return
    if kind == "selection":
        unique(data["case_ids"], "selection.case_ids")
        return
    if kind == "bundle":
        from inferyard.config.bundle import validate_bundle

        validate_bundle(data)
        return
    if kind == "score":
        if data["quality_state"] == "unscorable" and not data["reason"]:
            raise ContractError("score.reason", "unscorable output requires reason")
        if (data["category"] in ("performance", "svg")) != (
            data["quality_state"] == "not_applicable"
        ):
            raise ContractError("score.quality_state", "unscored category must be not applicable")
        return
    if kind in ("manifest", "summary"):
        from inferyard.contracts.contracts_storage import validate_storage

        validate_storage(kind, data)
        return
    if kind in ("closed_concurrency", "native_tools", "total_observer_control"):
        from importlib import import_module

        import_module("inferyard.extensions." + kind).validate_spec(data)
        return
    hooks = {"experiment": _experiment, "plan": _plan, "run": _run}
    if kind in hooks:
        hooks[kind](data)
    elif kind == "metric_observation":
        _observation(data)
    elif kind == "metric_definition":
        for ref in data["evidence_refs"]:
            _reference(ref, "metric_definition.evidence_refs")
        if data["implementation_status"] == "verified" and not data["evidence_refs"]:
            raise ContractError("metric_definition.evidence_refs", "verification requires evidence")
        unique(data["required_capabilities"], "metric_definition.required_capabilities")
    elif kind == "analysis":
        unique([r["run_id"] for r in data["source_runs"]], "analysis.source_runs")
        if data["analysis_id"] == data["parent_analysis_id"]:
            raise ContractError("analysis.parent_analysis_id", "cannot reference self")
        sources = {r["run_id"] for r in data["source_runs"]}
        for metric in data["metrics"]:
            _observation(metric)
            if "analysis_id" in metric:
                if metric["analysis_id"] != data["analysis_id"] or not set(
                    metric["source_run_ids"]
                ).issubset(sources):
                    raise ContractError(
                        "analysis.metrics.analysis_id", "aggregate source binding mismatch"
                    )
            elif metric["run_id"] not in sources:
                raise ContractError("analysis.metrics.run_id", "unknown source run")
