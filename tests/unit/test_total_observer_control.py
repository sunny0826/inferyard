import asyncio
import hashlib
from copy import deepcopy

import pytest

from inferyard.evidence.storage import EvidenceError, json_bytes
from inferyard.extensions.total_observer_control import COVERAGE, assess_total, run_total


def packet():
    spec = {
        "schema_version": 3,
        "definition": "total_observer_control.v1",
        "case_ids": ["a", "b"],
        "tolerance_ratio": 0.05,
        "max_wall_seconds": 10,
        "max_guard_gap_ns": 2_000_000_000,
        "tool_source_sha256": "a" * 64,
        "config_sha256": "b" * 64,
        "bundle_sha256": "c" * 64,
        "guardian_policy_sha256": "d" * 64,
    }
    key = hashlib.sha256(json_bytes(spec)).hexdigest()
    arms = []
    for i, mode in enumerate(["off", "on", "on", "off"]):
        begin = (i + 1) * 1_000_000_000
        arms.append(
            {
                "mode": mode,
                "tool_source_sha256": "a" * 64,
                "config_sha256": "b" * 64,
                "bundle_sha256": "c" * 64,
                "protocol_sha256": key,
                "client_pid": 100,
                "clock_id": "clock",
                "begin_ns": begin,
                "after_close_ns": begin + 800_000_000,
                "finalized": True,
                "coverage": list(COVERAGE) if mode == "on" else [],
                "requests": [
                    {
                        "case_id": c,
                        "state": "completed",
                        "duration_ns": 100_000_000,
                        "output_sha256": "e" * 64,
                    }
                    for c in ["a", "b"]
                ],
            }
        )
    guard = {
        "policy_sha256": "d" * 64,
        "clock_id": "clock",
        "pid": 200,
        "samples": [
            {
                "monotonic_ns": i * 1_000_000_000,
                "safe": True,
                "environment_identity": {"host": "stable"},
            }
            for i in range(6)
        ],
    }
    return spec, arms, guard


def test_identical_full_lifecycles_pass_software_but_not_hardware():
    spec, arms, guard = packet()
    result = assess_total(spec, arms, guard)
    assert result["valid_control"] and result["within_tolerance"]
    assert result["pairs"][0]["full_lifecycle_change_ratio"] == 0
    assert not result["hardware_qualified"]


def test_live_control_needs_full_guard_identity_environment_and_bound_policy():
    spec, arms, guard = packet()
    result = assess_total(spec, arms, guard, evidence_kind="live")
    assert not result["hardware_qualified"]
    assert "independent_guard_process_identity_missing" in result["reasons"]
    guard["policy"] = {"fixture_policy": True}
    spec["guardian_policy_sha256"] = guard["policy_sha256"] = hashlib.sha256(
        json_bytes(guard["policy"])
    ).hexdigest()
    guard["process_start_ticks"] = 1
    for sample in guard["samples"]:
        sample["environment_identity"] = dict.fromkeys(
            ("platform", "kernel", "cpu_model", "ac_online", "governor", "epp"), "fixture"
        )
    for arm in arms:
        arm["protocol_sha256"] = hashlib.sha256(json_bytes(spec)).hexdigest()
    assert assess_total(spec, arms, guard, evidence_kind="live")["hardware_qualified"]


def test_final_seal_cost_outside_request_latency_is_included_and_can_fail():
    spec, arms, guard = packet()
    arms[1]["after_close_ns"] += 100_000_000
    result = assess_total(spec, arms, guard)
    assert result["valid_control"] and not result["within_tolerance"]
    assert result["pairs"][0]["request_median_change_ratio"] == 0
    assert result["pairs"][0]["full_lifecycle_change_ratio"] == 0.125


@pytest.mark.parametrize(
    "change,reason",
    [
        ("same_pid", "independent_process"),
        ("gap", "guard_gap"),
        ("unsafe", "safety_not_verified"),
        ("window", "cover_full_arm"),
        ("environment", "environment_changed"),
        ("clock", "clock_not_bound"),
        ("coverage", "coverage_incomplete"),
        ("output", "output_work_differs"),
    ],
)
def test_incomplete_or_confounding_control_refuses_qualification(change, reason):
    spec, arms, guard = packet()
    if change == "same_pid":
        guard["pid"] = 100
    if change == "gap":
        guard["samples"] = guard["samples"][::3]
    if change == "unsafe":
        guard["samples"][2]["safe"] = False
    if change == "window":
        guard["samples"] = guard["samples"][2:]
    if change == "environment":
        guard["samples"][2]["environment_identity"] = {"host": "other"}
    if change == "clock":
        guard["clock_id"] = "other"
    if change == "coverage":
        arms[1]["coverage"].remove("fsync")
    if change == "output":
        arms[1]["requests"][0]["output_sha256"] = "f" * 64
    result = assess_total(spec, arms, guard, evidence_kind="live")
    assert not result["hardware_qualified"]
    assert any(reason in r for r in result["reasons"])


def test_source_mismatch_and_overlapping_arms_are_evidence_errors():
    spec, arms, guard = packet()
    arms[1]["tool_source_sha256"] = "x" * 64
    with pytest.raises(EvidenceError, match="identity_mismatch"):
        assess_total(spec, arms, guard)
    spec, arms, guard = packet()
    arms[1]["begin_ns"] = arms[0]["begin_ns"]
    with pytest.raises(EvidenceError, match="overlap"):
        assess_total(spec, arms, guard)


def test_driver_keeps_abba_order_and_times_close_with_no_retry():
    spec, _, _ = packet()
    events = []
    counter = iter(range(1000))

    class Observer:
        def __init__(self, mode):
            self.mode = mode

        async def open(self):
            events.append((self.mode, "open"))

        async def close(self):
            events.append((self.mode, "close"))

    class Guard:
        clock_id = "clock"

        def stopped(self):
            return False

    async def workload(observer):
        events.append((observer.mode, "work"))
        return []

    arms = asyncio.run(run_total(spec, workload, Observer, Guard(), clock=lambda: next(counter)))
    assert [a["mode"] for a in arms] == ["off", "on", "on", "off"]
    assert all(a["finalized"] and a["after_close_ns"] > a["begin_ns"] for a in arms)
    assert events == [(m, e) for m in ["off", "on", "on", "off"] for e in ["open", "work", "close"]]


def test_failed_workload_closes_observer_and_checkpoints_partial_arm():
    spec, _, _ = packet()
    saved = []
    closed = []

    class Observer:
        def __init__(self, mode):
            pass

        async def open(self):
            pass

        async def close(self):
            closed.append(True)

    class Guard:
        clock_id = "clock"

        def stopped(self):
            return False

    async def fail(observer):
        raise RuntimeError("fixture failure")

    with pytest.raises(RuntimeError):
        asyncio.run(
            run_total(
                spec, fail, Observer, Guard(), arm_sink=lambda arms: saved.append(deepcopy(arms))
            )
        )
    assert closed == [True] and len(saved[-1]) == 1 and not saved[-1][0]["finalized"]
    assert saved[-1][0]["after_close_ns"] > saved[-1][0]["begin_ns"]
