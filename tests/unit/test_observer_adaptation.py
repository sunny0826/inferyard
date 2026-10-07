"""Partial observations keep failure evidence; v1 keeps its original disk meaning."""

import copy
import json

import pytest

from tests.unit.test_observer_analysis import bridge as observer_bridge
from tests.unit.test_observer_analysis import records, save

bridge = observer_bridge


@pytest.mark.parametrize("with_end", [True, False])
def test_partial_requires_explicit_option_and_retains_coverage(bridge, tmp_path, with_end):
    values = records()
    values[-1]["stop_reason"] = "cancelled"
    values[-1]["elapsed_ns"] = values[2]["read_finished_ns"]
    if not with_end:
        values.pop()
    path = save(tmp_path, values)
    with pytest.raises(bridge.ObserverError):
        bridge.analyze(path)
    result = bridge.analyze(path, allow_incomplete=True)
    assert result["completeness"] == "incomplete"
    assert result["samples"] == 2
    assert result["end_record_present"] is with_end
    assert result["process"]["cpu_coverage_ns"] == 1_000_000_000
    assert result["performance_comparison_qualified"] is False
    if not with_end:
        assert result["elapsed_ns"] is result["skipped_intervals"] is None
        assert result["elapsed_missing_reason"] == "end_record_missing"
    out = tmp_path / "partial.json"
    assert bridge.main(["--log", str(path), "--out", str(out), "--allow-incomplete"]) == 3
    assert json.loads(out.read_text())["completeness"] == "incomplete"


def lost_values(who, state="exited", reason="process_exited"):
    values = records()
    row = values[2]["processes"][0] if who == "process" else values[2]["observer"]
    row["state"] = state
    for key in ("cpu_total_ns", "rss_bytes", "cpu_percent_one_core"):
        row[key] = {
            "value": None,
            "reason": reason,
            "source": "counter_delta_over_monotonic_interval"
            if key == "cpu_percent_one_core"
            else "native_process",
        }
    values[-1]["stop_reason"] = "target_unavailable" if who == "process" else "observer_unavailable"
    return values


@pytest.mark.parametrize("who", ["process", "observer"])
@pytest.mark.parametrize(
    ("state", "reason"),
    [
        ("exited", "process_exited"),
        ("identity_changed", "process_identity_changed"),
        ("unavailable", "permission_denied"),
    ],
)
def test_lost_target_is_null_with_reason(bridge, tmp_path, who, state, reason):
    path = save(tmp_path, lost_values(who, state, reason))
    result = bridge.analyze(path, allow_incomplete=True)
    assert result[who]["last_state"] == state
    assert result[who]["stop_reason"] == reason
    assert result[who]["rss_missing_samples"] == 1
    assert result[who]["mean_cpu_percent_one_core"] is None


@pytest.mark.parametrize("case", ["known_value", "wrong_reason", "identity", "end", "resume"])
def test_partial_mode_still_rejects_invalid_lost_target(bridge, tmp_path, case):
    values = lost_values("process")
    row = values[2]["processes"][0]
    if case == "known_value":
        row["rss_bytes"].update(value=10, reason=None)
    elif case == "wrong_reason":
        row["rss_bytes"]["reason"] = "permission_denied"
    elif case == "identity":
        row["process_start_ticks"] += 1
    elif case == "end":
        values[-1]["stop_reason"] = "cancelled"
    else:
        resumed = copy.deepcopy(records()[2])
        resumed["seq"] = 3
        values.insert(-1, resumed)
        values[-1]["samples"] = 3
    with pytest.raises(bridge.ObserverError):
        bridge.analyze(save(tmp_path, values), allow_incomplete=True)


@pytest.mark.parametrize("case", ["seq", "source", "counter", "truncated", "mixed"])
def test_partial_mode_does_not_accept_corruption(bridge, tmp_path, case):
    values = records()
    values.pop()
    if case == "seq":
        values[2]["seq"] = 3
    elif case == "source":
        values[2]["host"]["disk_available_bytes"]["source"] = "other"
    elif case == "counter":
        values[2]["processes"][0]["cpu_percent_one_core"]["value"] = 88
    elif case == "mixed":
        values[2]["definition"] = "lab_observer.v1"
    path = save(tmp_path, values)
    if case == "truncated":
        path.write_bytes(path.read_bytes()[:-1])
    with pytest.raises(bridge.ObserverError):
        bridge.analyze(path, allow_incomplete=True)


def test_v2_disk_scope_and_v1_rejection(bridge, tmp_path):
    values = records()
    path = save(tmp_path, values)
    current = bridge.analyze(path)
    assert current["input_definition"] == "lab_observer.v2"
    assert current["disk_scope"] == "observer_cwd_filesystem"
    for item in values:
        item["definition"] = "lab_observer.v1"
    with pytest.raises(bridge.UnsupportedFormat, match="unsupported_format"):
        bridge.analyze(save(tmp_path, values))
    out = tmp_path / "unsupported-summary.json"
    assert bridge.main(["--log", str(path), "--out", str(out)]) == 2
    assert not out.exists()


def test_partial_header_only_is_not_an_observation(bridge, tmp_path):
    with pytest.raises(bridge.ObserverError):
        bridge.analyze(save(tmp_path, records()[:1]), allow_incomplete=True)
