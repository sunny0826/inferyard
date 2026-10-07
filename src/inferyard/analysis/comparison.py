"""Explicit frozen comparison modes; unknown evidence never grants eligibility."""

import math

from inferyard.analysis.comparison_helpers import _get, _known, comparable_arguments
from inferyard.evidence.storage import EvidenceError

FACTORS = {
    "cache_policy": ("conditions.cache_policy", ()),
    "threads": ("conditions.threads", ("-t", "--threads")),
    "threads_batch": ("conditions.threads_batch", ("-tb", "--threads-batch")),
    "context_size": ("conditions.context_size", ("-c", "--ctx-size")),
    "quantization": ("model.packing", ("-m", "--model")),
}


def normalized_args(args, excluded=()):
    result, skip = [], False
    for arg in comparable_arguments(args):
        if skip:
            skip = False
        elif arg in excluded:
            skip = True
        elif arg.split("=", 1)[0] in excluded:
            continue
        else:
            result.append(arg)
    return result


def compare_trials(left, right, *, mode=None, performance_evidence=None, definition="phase2.v3"):
    from inferyard.evidence.formats import require_version

    require_version({"definition": definition}, "definition", ("phase2.v3",), "comparison")
    declared = [d["plan"]["experiment"]["comparison"] for d in (left, right)]
    chosen = mode or (declared[0]["mode"] if declared[0] == declared[1] else "side-by-side")
    if chosen not in ("model", "config", "side-by-side"):
        raise EvidenceError("unsupported_comparison_mode")
    conditions = []

    def check(field, a, b, *, allowed=False, impact="common"):
        if field in ("run.definition_versions.scoring", "selection.scorer_sha256"):
            impact = "quality"
        elif field == "run.definition_versions.measurement":
            impact = "performance"
        known = _known(a) and _known(b)
        state = (
            "unknown"
            if not known
            else "same"
            if a == b
            else "allowed_difference"
            if allowed
            else "different"
        )
        conditions.append(
            {"field": field, "left": a, "right": b, "status": state, "impact": impact}
        )

    frozen = declared[0] == declared[1] and declared[0]["mode"] == chosen
    if chosen != "side-by-side" and not frozen:
        check("frozen_comparison_mode", declared[0], declared[1])
        check("requested_mode_matches_plan", chosen, None)
    factor = declared[0]["factor"] if chosen == "config" and frozen else None
    canonical = factor.removeprefix("conditions.") if isinstance(factor, str) else None
    factor_definition = FACTORS.get(canonical) if chosen == "config" else None
    if chosen == "config" and factor_definition is None:
        check("supported_config_factor", factor, None)
    allowed_path, excluded_args = factor_definition or (None, ())
    quantization = chosen == "config" and canonical == "quantization"
    quantization_gate = None
    if quantization:
        from inferyard.analysis.quantization_comparison import qualification

        quantization_gate = qualification(left, right)
        check(
            "same_base_quantization",
            "verified" if quantization_gate["eligible"] else None,
            "verified",
        )
    # Null thresholds explicitly disable individual checks; they are not missing evidence.
    policies = [
        {
            k: "disabled" if v is None else v
            for k, v in d["plan"]["experiment"].get("safety", {}).items()
        }
        or "disabled"
        for d in (left, right)
    ]
    check("safety_policy", *policies)
    check(
        "performance_environment_policy",
        *[
            d["plan"]["experiment"].get("performance_environment", "not_frozen")
            for d in (left, right)
        ],
        impact="performance",
    )
    check(
        "capacity_stop_policy",
        *[d["plan"]["experiment"].get("capacity_stop", "disabled") for d in (left, right)],
    )
    resource_policies = [d["plan"]["experiment"].get("resource_comparison") for d in (left, right)]
    if any(p is not None for p in resource_policies):
        check("resource_window_policy", *resource_policies, impact="performance")
    for path in (
        "run.definition_versions",
        "run.tool_source_sha256",
        "selection.bundle_sha256",
        "selection.scorer_sha256",
        "selection.case_ids",
        "config.device",
        "config.engine.adapter",
        "config.engine.release",
        "config.engine.binary_sha256",
        "config.engine.backend",
        "identity.runtime_library_hashes",
        "environment_start.platform",
        "environment_start.architecture",
        "environment_start.cpu_model",
        "environment_start.kernel",
    ):
        if path == "run.tool_source_sha256" and any(
            "implementation_identity" in d["run"] for d in (left, right)
        ):
            from inferyard.implementation_identity import role_identities

            identities = [
                role_identities(d["run"].get("implementation_identity")) for d in (left, right)
            ]
            # New identities are never inferred from legacy full-package hashes.
            for role, impact in (("measurement", "performance"), ("scoring", "quality")):
                check(
                    "run.implementation_identity." + role,
                    *[identity[role] for identity in identities],
                    impact=impact,
                )
        elif path == "run.definition_versions":
            for component in ("measurement", "scoring"):
                field = path + "." + component
                check(field, _get(left, field), _get(right, field))
        else:
            check(path, _get(left, path), _get(right, path))
    for key in ("repo", "revision", "sha256", "packing", "template_sha256"):
        path = "config.model." + key
        check(
            path,
            _get(left, path),
            _get(right, path),
            allowed=chosen == "model"
            or quantization
            and key in ("repo", "revision", "sha256", "packing"),
        )
    for section in ("generation", "execution", "conditions", "telemetry"):
        a, b = left["config"][section], right["config"][section]
        for key in sorted(set(a) | set(b)):
            darwin = all(
                d.get("environment_start", {}).get("platform") == "Darwin" for d in (left, right)
            )
            if (
                section == "conditions"
                and darwin
                and key in ("governor", "epp", "require_epp_match")
            ):
                # These Linux fields remain unknown. The native policy below
                # and the observed environment qualification are still mandatory.
                continue
            path = section + "." + key
            impact = (
                "performance"
                if section == "telemetry"
                or key in ("ac_online", "profile", "governor", "epp", "background_load")
                else "common"
            )
            values = (a.get(key), b.get(key))
            if section == "conditions" and key == "macos_power_policy":
                from inferyard.platforms.power_macos import valid

                values = tuple(
                    {**v, "power_mode": "not_supported"}
                    if valid(v) and not v["power_mode_supported"]
                    else v
                    for v in values
                )
            check(
                "config." + path,
                *values,
                allowed=path == allowed_path,
                impact=impact,
            )
    check(
        "config.engine.startup_args",
        normalized_args(left["config"]["engine"]["startup_args"], excluded_args),
        normalized_args(right["config"]["engine"]["startup_args"], excluded_args),
    )
    for data, side in ((left, "left"), (right, "right")):
        check(side + ".identity_verified", _get(data, "identity.verification"), "verified")
        check(side + ".complete", data["summary"]["completeness"], "complete")
        trial = next(t for t in data["plan"]["trials"] if t["trial_id"] == data["run"]["trial_id"])
        workload = next(
            w
            for w in data["plan"]["experiment"]["workloads"]
            if w["workload_id"] == trial["workload_id"]
        )
        if canonical == "cache_policy" and chosen == "config":
            from inferyard.analysis.cache_comparison import qualification

            reasons = qualification(data, workload)
            check(side + ".cache_sequence_evidence", reasons or "verified", "verified")
        data = {
            **data,
            "comparison_workload": {
                k: workload[k]
                for k in (
                    "purpose",
                    "protocol",
                    "input_target_tokens",
                    "output_budget_tokens",
                    "timeout_seconds",
                )
            },
        }
        if workload["input_target_tokens"] is not None:
            check(
                side + ".input_target_match",
                _get(data, "summary.input_target_check.status"),
                "matched",
            )
        data["comparison_workload"]["input_tolerance_tokens"] = workload.get(
            "input_tolerance_tokens", 0
        )
        data["comparison_workload"]["position_cases"] = workload.get("position_cases", [])
        if "output_mode" in workload:
            data["comparison_workload"]["output_mode"] = workload["output_mode"]
        if "cache_protocol" in workload:
            data["comparison_workload"]["cache_protocol"] = workload["cache_protocol"]
        data["comparison_workload"]["repeat_case_orders"] = workload.get("repeat_case_orders", [])
        if side == "left":
            left_workload = data["comparison_workload"]
        else:
            # Null input target is an explicit protocol choice, not unknown evidence.
            a, b = dict(left_workload), dict(data["comparison_workload"])
            for record in (a, b):
                if record["input_target_tokens"] is None:
                    record["input_target_tokens"] = "not_targeted"
            check("task_protocol", a, b)
        effective = _get(data, "identity.effective_parameters.parameters") or {}
        for key in (
            "seed",
            "temperature",
            "top_k",
            "top_p",
            "min_p",
            "presence_penalty",
            "repeat_penalty",
            "max_tokens",
        ):
            observed = effective.get(key, {})
            requested = data["config"]["generation"][key]
            actual = observed.get("effective")
            if (
                observed.get("verification") == "verified"
                and type(actual) in (int, float)
                and math.isfinite(actual)
                and math.isclose(actual, requested, rel_tol=1e-6, abs_tol=1e-6)
            ):
                observed = {**observed, "effective": requested}
            check(
                side + ".effective." + key,
                observed.get("effective") if observed.get("verification") == "verified" else None,
                data["config"]["generation"][key],
            )
    blockers = [
        c["field"] + ":" + c["status"]
        for c in conditions
        if c["impact"] == "common" and c["status"] not in ("same", "allowed_difference")
    ]
    if chosen == "side-by-side":
        blockers.append("side_by_side_has_no_controlled_deltas")
    if (
        chosen == "config"
        and allowed_path
        and _get(left["config"], allowed_path) == _get(right["config"], allowed_path)
    ):
        blockers.append("declared_factor_did_not_change")
    common = not blockers
    quality_ready = common and not any(
        c["impact"] == "quality" and c["status"] not in ("same", "allowed_difference")
        for c in conditions
    )
    completion_ready = common and not any(
        c["field"]
        in ("run.definition_versions.measurement", "run.implementation_identity.measurement")
        and c["status"] != "same"
        for c in conditions
    )
    a, b = left["summary"]["completion_rate"]["value"], right["summary"]["completion_rate"]["value"]
    result = {
        "quality_differences": quality_differences(left, right, quality_ready),
        "mode": chosen,
        "factor": factor,
        "conditions": conditions,
        "eligibility": {
            "completion": completion_ready,
            "quality": quality_ready,
            "performance": False,
        },
        "completion_rate_difference": b - a
        if completion_ready and a is not None and b is not None
        else None,
        "blockers": blockers,
        "limitations": [
            "performance_environment_and_overhead_binding_pending",
            "no_automatic_winner_or_causal_attribution",
        ],
    }
    if quantization_gate is not None:
        result["quantization_qualification"] = quantization_gate
    if performance_evidence is not None:
        from inferyard.analysis.performance_comparison import performance_comparison

        performance = performance_comparison(
            left, right, result, performance_evidence, definition=definition
        )
        result["performance_analysis"] = performance
        result["eligibility"]["performance"] = performance["eligible"]
        result["limitations"] = performance["limitations"]
    if chosen == "config" and canonical == "cache_policy":
        result["limitations"].extend(
            [
                "cache_policy_difference_does_not_require_cache_hits",
                "ordered_probe_warmup_and_request_sequence_scope_only",
            ]
        )
    from inferyard.analysis.observed_comparison import observed_differences

    result["definition"] = definition
    result["observed_differences"] = observed_differences(left, right, result)
    result["calibration_status"] = (
        [
            {
                "status": "qualified" if p["eligible"] else "limited",
                "reasons": p["reasons"],
                "incremental_diagnostic": p.get("incremental_diagnostic"),
            }
            for p in performance_evidence
        ]
        if performance_evidence is not None
        else [
            {"status": "not_supplied", "reasons": ["performance_evidence_not_supplied"]}
            for _ in (left, right)
        ]
    )
    return result


def quality_differences(left, right, eligible):
    def index(data):
        return {
            (m["metric_id"], m["statistic"], m["group"]["category"]): m
            for m in data["summary"].get("metric_observations", [])
            if m["metric_id"].startswith("Q") and m["request_id"] is None
        }

    a, b = index(left), index(right)
    rows = []
    for key in sorted(set(a) | set(b), key=str):
        first, second = a.get(key), b.get(key)
        same = (
            first
            and second
            and all(
                first[k] == second[k] for k in ("definition_version", "unit", "source", "layer")
            )
        )
        known = same and first["value"] is not None and second["value"] is not None
        allowed = bool(
            eligible
            and known
            and first.get("comparison_eligible") is True
            and second.get("comparison_eligible") is True
        )
        rows.append(
            {
                "metric_id": key[0],
                "statistic": key[1],
                "category": key[2],
                "left": first["value"] if first else None,
                "right": second["value"] if second else None,
                "difference": second["value"] - first["value"] if allowed else None,
                "eligible": allowed,
                "unit": first["unit"] if same else None,
            }
        )
    return rows
