import importlib
from pathlib import Path

import pytest

from inferyard.evidence.storage import EvidenceError


@pytest.fixture
def profiler(monkeypatch):
    monkeypatch.syspath_prepend(str(Path(__file__).resolve().parents[2] / "scripts"))
    return importlib.import_module("observer_cost_profile")


def test_nested_cost_is_counted_once_and_never_grants_causal_eligibility(profiler):
    profile = profiler.ObserverCostProfile()
    profile.records = [
        {
            "component": "outer",
            "started_ns": 10,
            "finished_ns": 50,
            "thread_cpu_ns": 12,
            "nesting_depth": 0,
        },
        {
            "component": "inner",
            "started_ns": 20,
            "finished_ns": 40,
            "thread_cpu_ns": 8,
            "nesting_depth": 1,
        },
    ]
    value = profile.summarize(
        [{"case_id": "c", "request_id": "r", "t_send_ns": 0, "t_terminal_ns": 100}]
    )
    assert value["all_phases_union_wall_ns"] == 40
    assert value["all_phases_outer_thread_cpu_ns"] == 12
    assert value["formal_requests"][0]["profiled_wall_fraction"] == 0.4
    assert not value["causal_total_perturbation_qualified"]


def test_error_is_recorded_and_inherited_method_is_restored(profiler):
    class Parent:
        def operation(self):
            raise ValueError("expected")

    class Child(Parent):
        pass

    profile = profiler.ObserverCostProfile()
    profile.wrap(Child, "operation", "test")
    with pytest.raises(ValueError, match="expected"):
        Child().operation()
    assert profile.depth == 0
    assert profile.records[0]["outcome"] == "raised"
    profile.__exit__()
    assert "operation" not in vars(Child)
    assert Child.operation is Parent.operation


def test_empty_profile_is_not_zero_cost(profiler):
    with pytest.raises(EvidenceError, match="incomplete_observer_cost_profile"):
        profiler.ObserverCostProfile().summarize([])


def test_missing_outer_interval_is_rejected(profiler):
    profile = profiler.ObserverCostProfile()
    profile.records = [
        {
            "component": "inner",
            "started_ns": 10,
            "finished_ns": 20,
            "thread_cpu_ns": 1,
            "nesting_depth": 1,
        }
    ]
    with pytest.raises(EvidenceError, match="observer_cost_nesting_mismatch"):
        profile.summarize([])
