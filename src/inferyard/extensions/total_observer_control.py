"""All-observer ABBA contract with a common independent safety/environment guard.

Whole arm wall time includes setup and final flush/seal/close. The request E2E
assessment is separate. Method wall-time fractions never substitute for this gate.
"""

import asyncio
import hashlib
import time
from statistics import median

from inferyard.contracts.schemas_extensions import TOTAL_OBSERVER_CONTROL as SPEC
from inferyard.contracts.schemas_extensions import TOTAL_TRIAL_CONTROL
from inferyard.contracts.validation import _validate
from inferyard.evidence.storage import EvidenceError, json_bytes

ORDER = ("off", "on", "on", "off")
COVERAGE = (
    "setup",
    "safety",
    "boundary",
    "periodic",
    "json_validation",
    "append",
    "snapshot",
    "flush",
    "fsync",
    "seal",
    "close",
)


def validate_spec(spec):
    _validate(
        spec,
        TOTAL_TRIAL_CONTROL if spec.get("definition") == "total_observer_control.v2" else SPEC,
        "total_observer_control",
    )
    if len(set(spec["case_ids"])) != len(spec["case_ids"]) or spec["tolerance_ratio"] >= 1:
        raise EvidenceError("total_control_spec_invalid")


def assess_total(spec, arms, guard, *, evidence_kind="fixture"):
    validate_spec(spec)
    reasons, pairs = [], []
    if evidence_kind not in ("fixture", "live"):
        raise EvidenceError("total_control_kind_invalid")
    if len(arms) != 4 or [a.get("mode") for a in arms] != list(ORDER):
        raise EvidenceError("total_control_four_abba_arms_required")
    protocol_hash = hashlib.sha256(json_bytes(spec)).hexdigest()
    if guard.get("policy_sha256") != spec["guardian_policy_sha256"]:
        raise EvidenceError("total_control_guard_policy_mismatch")
    samples = guard.get("samples", [])
    if len(samples) < 2:
        reasons.append("independent_guard_samples_missing")
    previous = None
    for sample in samples:
        timestamp = sample.get("monotonic_ns")
        if (
            type(timestamp) is not int
            or timestamp < 0
            or previous is not None
            and timestamp <= previous
        ):
            raise EvidenceError("total_control_guard_clock_invalid")
        if previous is not None and timestamp - previous > spec["max_guard_gap_ns"]:
            reasons.append("independent_guard_gap")
        previous = timestamp
        if sample.get("safe") is not True:
            reasons.append("independent_guard_safety_not_verified")
    environments = [s.get("environment_identity") for s in samples]
    if not environments or any(
        not isinstance(e, dict) or not e or any(v is None for v in e.values()) for e in environments
    ):
        reasons.append("independent_guard_environment_unknown")
    elif any(e != environments[0] for e in environments[1:]):
        reasons.append("independent_guard_environment_changed")
    if evidence_kind == "live":
        required = {"platform", "kernel", "cpu_model", "ac_online", "governor", "epp"}
        from inferyard.analysis.environment_identity import MACOS_FIELDS
        from inferyard.platforms.power_macos import valid

        if any(
            not isinstance(e, dict)
            or not (
                set(MACOS_FIELDS) <= set(e) and valid(e.get("macos_power_policy"))
                if e.get("platform") == "Darwin"
                else required <= set(e)
            )
            for e in environments
        ):
            reasons.append("independent_guard_environment_unknown")
        policy = guard.get("policy")
        if (
            not isinstance(policy, dict)
            or hashlib.sha256(json_bytes(policy)).hexdigest() != spec["guardian_policy_sha256"]
        ):
            reasons.append("independent_guard_policy_not_authenticated")
        if type(guard.get("process_start_ticks")) is not int or guard["process_start_ticks"] <= 0:
            reasons.append("independent_guard_process_identity_missing")
    for mode, arm in zip(ORDER, arms, strict=True):
        if (
            any(
                arm.get(k) != spec[k]
                for k in ("tool_source_sha256", "config_sha256", "bundle_sha256")
            )
            or arm.get("protocol_sha256") != protocol_hash
        ):
            raise EvidenceError("total_control_arm_identity_mismatch")
        if arm.get("clock_id") != guard.get("clock_id") or not guard.get("clock_id"):
            reasons.append("independent_guard_clock_not_bound")
        if (
            type(guard.get("pid")) is not int
            or type(arm.get("client_pid")) is not int
            or guard["pid"] == arm["client_pid"]
        ):
            reasons.append("guard_not_independent_process")
        coverage = [
            *COVERAGE,
            *(
                ["offline_reconstruction"]
                if spec["definition"] == "total_observer_control.v2"
                else []
            ),
        ]
        if arm.get("coverage") != (coverage if mode == "on" else []):
            reasons.append("total_observer_coverage_incomplete")
        if spec["definition"] == "total_observer_control.v2" and (
            arm.get("measurement_path") != "run_trial.v1"
            or arm.get("trial_plan_sha256") != spec["trial_plan_sha256"]
        ):
            reasons.append("total_control_formal_trial_path_not_bound")
        start, end = arm.get("begin_ns"), arm.get("after_close_ns")
        if type(start) is not int or type(end) is not int or end <= start:
            raise EvidenceError("total_control_arm_window_invalid")
        if not samples or samples[0]["monotonic_ns"] > start or samples[-1]["monotonic_ns"] < end:
            reasons.append("independent_guard_does_not_cover_full_arm")
        rows = arm.get("requests", [])
        if [r.get("case_id") for r in rows] != spec["case_ids"]:
            raise EvidenceError("total_control_case_order_mismatch")
        if not arm.get("finalized"):
            reasons.append("total_control_arm_not_finalized")
        for row in rows:
            if (
                row.get("state") != "completed"
                or type(row.get("duration_ns")) is not int
                or row["duration_ns"] <= 0
                or not row.get("output_sha256")
            ):
                reasons.append("total_control_requests_incomplete")
        if rows and sum(r.get("duration_ns", 0) for r in rows) > end - start:
            raise EvidenceError("total_control_request_duration_outside_arm")
    for a, b in zip(arms, arms[1:], strict=False):
        if a["after_close_ns"] > b["begin_ns"]:
            raise EvidenceError("total_control_arm_overlap")
    if (arms[-1]["after_close_ns"] - arms[0]["begin_ns"]) / 1e9 > spec["max_wall_seconds"]:
        reasons.append("total_control_frozen_wall_budget_exceeded")
    for index in range(len(spec["case_ids"])):
        if len({a["requests"][index].get("output_sha256") for a in arms}) != 1:
            reasons.append("total_control_output_work_differs")
    if not reasons:
        for off, on in ((0, 1), (3, 2)):
            a, b = arms[off], arms[on]
            request_changes = [
                (q["duration_ns"] - p["duration_ns"]) / p["duration_ns"]
                for p, q in zip(a["requests"], b["requests"], strict=True)
            ]
            full_a, full_b = (
                a["after_close_ns"] - a["begin_ns"],
                b["after_close_ns"] - b["begin_ns"],
            )
            pairs.append(
                {
                    "off_arm": off + 1,
                    "on_arm": on + 1,
                    "request_median_change_ratio": median(request_changes),
                    "full_lifecycle_change_ratio": (full_b - full_a) / full_a,
                }
            )
    within = bool(pairs) and all(
        abs(p[k]) <= spec["tolerance_ratio"]
        for p in pairs
        for k in ("request_median_change_ratio", "full_lifecycle_change_ratio")
    )
    return {
        "definition": spec["definition"],
        "evidence_kind": evidence_kind,
        "valid_control": not reasons,
        "within_tolerance": within,
        "pairs": pairs,
        "reasons": sorted(set(reasons)),
        "hardware_qualified": evidence_kind == "live" and not reasons and within,
        "tolerance_ratio": spec["tolerance_ratio"],
        "limitations": [
            "shared_independent_guard_cost_is_common_not_estimated",
            "declared_cases_and_machine_only",
            "not_population_confidence",
            "direct_method_fraction_is_not_causal_slowdown",
        ],
    }


async def run_total(
    spec, workload, observer_factory, guard, *, clock=time.monotonic_ns, arm_sink=None
):
    """Executable four-arm driver. guard lives in another OS process in live mode.

    workload(observer) is identical in each arm, including probe/warmup/history.
    Observer callbacks are the sole factor. close must flush and seal on arms.
    The caller retains incomplete arms on error; this function never retries.
    """
    validate_spec(spec)
    import os

    arms = []
    protocol_hash = hashlib.sha256(json_bytes(spec)).hexdigest()
    async with asyncio.timeout(spec["max_wall_seconds"]):
        for mode in ORDER:
            if guard.stopped():
                break
            arm = {
                "mode": mode,
                "client_pid": os.getpid(),
                "clock_id": guard.clock_id,
                "begin_ns": clock(),
                "finalized": False,
                "requests": [],
                "protocol_sha256": protocol_hash,
                "coverage": [
                    *COVERAGE,
                    *(
                        ["offline_reconstruction"]
                        if spec["definition"] == "total_observer_control.v2"
                        else []
                    ),
                ]
                if mode == "on"
                else [],
                **{k: spec[k] for k in ("tool_source_sha256", "config_sha256", "bundle_sha256")},
            }
            arms.append(arm)
            if arm_sink:
                arm_sink(arms)
            observer = observer_factory(mode)
            try:
                await observer.open()
                arm["requests"] = await workload(observer)
            finally:
                await observer.close()
                arm["after_close_ns"] = clock()
                if hasattr(observer, "deferred_evidence"):
                    arm.update(observer.deferred_evidence())
                if arm_sink:
                    arm_sink(arms)
            arm["finalized"] = True
            if arm_sink:
                arm_sink(arms)
    return arms
