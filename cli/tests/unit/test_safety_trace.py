"""Trace failures and actual gaps without turning a polling interval into a guarantee."""

from copy import deepcopy
from types import SimpleNamespace

import pytest

from inferyard.evidence.storage import EvidenceError
from inferyard.platforms.identity import PreflightError
from inferyard.runtime.safety import SafetyGuard
from inferyard.runtime.safety_trace import SafetyTrace
from tests.unit.test_safety import Sensors


class Store:
    run_id = "run"
    clock_id = "clock"

    def __init__(self):
        self.rows = []

    def observation(self, name, row):
        assert name == "safety-checks.jsonl"
        self.rows.append(deepcopy(row))


def test_actual_periodic_gaps_survive_intervening_boundary_check():
    times = iter([10, 20, 30, 40, 2_000_000_010, 2_000_000_020])
    store = Store()
    trace = SafetyTrace(store, 1, clock=lambda: next(times))
    safety = SimpleNamespace(last=None)
    trace.check(lambda: None, safety, periodic=True)
    trace.check(lambda: None, safety)
    trace.check(lambda: None, safety, periodic=True)
    assert [r["sequence"] for r in store.rows] == [1, 2, 3]
    assert store.rows[-1]["periodic_start_gap_ns"] == 2_000_000_000
    assert store.rows[-1]["configured_interval_ns"] == 1_000_000_000
    assert store.rows[1]["periodic_start_gap_ns"] is None
    assert all(r["clock_id"] == "clock" for r in store.rows)


def test_prerequisite_failure_never_reuses_previous_temperature():
    store = Store()
    safety = SimpleNamespace(last={"temperature_samples": [{"value": 40}]})

    def fail():
        raise PreflightError("disk_safety_budget_reached")

    with pytest.raises(PreflightError, match="disk_safety"):
        SafetyTrace(store, 1).check(fail, safety)
    assert store.rows[0]["observation"] is None
    assert store.rows[0]["error"]["reason"] == "disk_safety_budget_reached"


def test_hot_reading_is_retained_and_still_raises():
    store = Store()
    safety = SimpleNamespace(last=None)

    def fail():
        safety.last = {"temperature_samples": [{"value": 91}]}
        raise PreflightError("temperature_safety_threshold_reached")

    with pytest.raises(PreflightError, match="temperature_safety"):
        SafetyTrace(store, 1).check(fail, safety, periodic=True)
    row = store.rows[0]
    assert row["outcome"] == "error"
    assert row["observation"]["temperature_samples"][0]["value"] == 91
    assert row["check_finished_ns"] >= row["check_started_ns"]


def test_environment_failure_keeps_this_attempt_sensor_reading():
    def fail():
        raise RuntimeError("private exception text")

    guard = SafetyGuard(
        {"check_environment": True, "max_external_cpu_percent": None},
        {},
        fail,
        sensors=Sensors(55),
    )
    store = Store()
    with pytest.raises(RuntimeError):
        SafetyTrace(store, 1).check(guard.check, guard)
    row = store.rows[0]
    assert row["observation"]["temperature_samples"] == [{"value": 55}]
    assert row["observation"]["environment"] is None
    assert row["error"] == {"type": "RuntimeError", "reason": None}


def test_evidence_write_error_is_not_silently_ignored():
    class Broken(Store):
        def observation(self, name, row):
            raise EvidenceError("evidence_append_failed")

    with pytest.raises(EvidenceError, match="evidence_append_failed"):
        SafetyTrace(Broken(), 1).check(lambda: None, SimpleNamespace(last=None))
