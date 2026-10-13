from copy import deepcopy

import pytest

from inferyard.evidence.storage import EvidenceError
from inferyard.platforms.external_cpu import SOURCE, capture, reduce_external_cpu
from tests.unit.test_resources import proc

ENDPOINT = {"server_pid": 77, "process_start_ticks": 55}


def record(t, host, service):
    return {
        "source": SOURCE,
        **ENDPOINT,
        "boot_id": "boot-1",
        "clock_ticks_per_second": 100,
        "phase": "formal",
        "request_id": "request-1",
        "read_started_ns": t,
        "read_finished_ns": t,
        "host_ticks": host,
        "service_ticks": service,
        "missing_reason": None,
    }


def rows():
    return [
        record(0, [100, 0, 20, 100, 0, 0, 0, 0], 10),
        record(1_000_000_000, [150, 0, 40, 230, 0, 0, 0, 0], 40),
    ]


def test_residual_busy_uses_host_capacity_and_keeps_raw_scope():
    result = reduce_external_cpu(rows(), ENDPOINT, 1000)
    assert result["observed_max_percent"] == 20  # (70 busy - 30 service) / 200 total
    assert result["valid_intervals"] == 1
    assert result["intervals"][0]["missing_reason"] == "baseline_only"
    assert not result["comparison_eligible"]


@pytest.mark.parametrize(
    "change,reason",
    [
        ("counter", "counter_regression"),
        ("skew", "host_process_counter_skew"),
        ("gap", "sampling_gap"),
        ("boot", "counter_identity_changed"),
        ("missing", "previous_sample_missing"),
    ],
)
def test_invalid_interval_is_missing_not_clamped_zero(change, reason):
    data = rows()
    if change == "counter":
        data[1]["host_ticks"][0] = 99
    elif change == "skew":
        data[1]["service_ticks"] = 1000
    elif change == "gap":
        data[1].update(read_started_ns=3_000_000_000, read_finished_ns=3_000_000_000)
    elif change == "boot":
        data[1]["boot_id"] = "boot-2"
    else:
        data[0].update(host_ticks=None, service_ticks=None, missing_reason="unavailable")
    result = reduce_external_cpu(data, ENDPOINT, 1000)
    assert result["intervals"][1]["missing_reason"] == reason
    assert result["observed_max_percent"] is None


def test_source_capture_and_pid_reuse(tmp_path):
    proc(tmp_path)
    (tmp_path / "stat").write_text("cpu 100 0 20 100 0 0 0 0 10 5\n")
    value = capture(77, 55, "boot-1", 100, "formal", "r", lambda: 10, tmp_path)
    assert value["host_ticks"] == [100, 0, 20, 100, 0, 0, 0, 0]
    assert value["service_ticks"] == 30
    proc(tmp_path, ticks=56)
    missing = capture(77, 55, "boot-1", 100, "formal", "r", lambda: 11, tmp_path)
    assert missing["host_ticks"] is None and missing["missing_reason"]
    altered = deepcopy(value)
    altered["server_pid"] += 1
    with pytest.raises(EvidenceError, match="binding"):
        reduce_external_cpu([altered], ENDPOINT, 1000)
