from copy import deepcopy

import pytest

from inferyard.analysis.boundary_environment import qualify_boundary_environment
from inferyard.evidence.storage import EvidenceError, json_bytes
from inferyard.runtime.boundary_observer import boundary_contract
from tests.unit.test_environment import state
from tests.unit.test_external_cpu import ENDPOINT, rows


def fixture(tmp_path, change=None):
    snapshot = state()
    cpu = rows()
    records = [
        {
            "phase": "formal",
            "request_id": "request-1",
            "boundary": boundary,
            "read_started_ns": raw["read_started_ns"],
            "read_finished_ns": raw["read_finished_ns"],
            "environment": deepcopy(snapshot),
            "cpu": raw,
        }
        for boundary, raw in zip(("before_send", "after_terminal"), cpu, strict=True)
    ]
    request = {
        "request_id": "request-1",
        "execution_state": "completed",
        "t_send_ns": 100_000_000,
        "t_terminal_ns": 900_000_000,
    }
    policy = {"max_external_cpu_percent": 25, "max_external_interval_seconds": 1}
    if change == "inside":
        request["t_send_ns"] = -1
    elif change == "duplicate":
        records.insert(1, deepcopy(records[0]))
    elif change == "missing":
        records.pop()
    elif change == "changed":
        records[-1]["environment"]["cpu_model"] = "other"
    elif change == "load":
        policy["max_external_cpu_percent"] = 10
    elif change == "width":
        policy["max_external_interval_seconds"] = 0.5
    elif change == "binding":
        records[-1]["cpu"]["request_id"] = "other"
    files = {
        "boundary-observer.json": json_bytes(boundary_contract()),
        "request-environment.jsonl": b"".join(json_bytes(r) for r in records),
        "environment.start.json": json_bytes(snapshot),
        "environment.end.json": json_bytes(snapshot),
    }
    for name, value in files.items():
        (tmp_path / name).write_bytes(value)
    data = {
        "requests": [request],
        "config": {"endpoint": ENDPOINT, "conditions": {}},
        "plan": {
            "experiment": {"performance_environment": None if change == "unfrozen" else policy}
        },
    }
    return qualify_boundary_environment(tmp_path, data, files)


def test_outside_request_bracket_is_bounded_observation_not_continuous(tmp_path):
    result = fixture(tmp_path)
    assert result["eligible"], result["reasons"]
    assert result["request_windows"][0]["coverage_ratio"] == 1
    assert result["request_windows"][0]["observed_max_percent"] == 20
    assert "environment_changes_between_boundaries_unknown" in result["limitations"]


@pytest.mark.parametrize(
    "change", ["inside", "duplicate", "missing", "changed", "load", "width", "unfrozen"]
)
def test_missing_changed_or_inside_request_observations_do_not_qualify(tmp_path, change):
    assert not fixture(tmp_path, change)["eligible"]


def test_nested_cpu_identity_cannot_be_reassigned(tmp_path):
    with pytest.raises(EvidenceError, match="binding_mismatch"):
        fixture(tmp_path, "binding")
