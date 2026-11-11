"""macOS additive capabilities retain Linux evidence and reject false source labels."""

from copy import deepcopy

import pytest

from inferyard.analysis.idle_rss import MACOS_SOURCE, idle_rss_observations
from inferyard.contracts.validation import Document
from inferyard.errors import ContractError, EvidenceError
from inferyard.runtime.collector_overhead import validate_protocol
from tests.unit.test_collector_overhead import protocol
from tests.unit.test_observations import EVIDENCE, RUN
from tests.unit.test_resources import CONFIG, cpu_sample
from tests.unit.test_stability_observations import idle_inputs


def macos_cpu():
    return {
        **cpu_sample(1, 1_000_000),
        "collector": "macos-resource.v1",
        "source": "psutil:Process.cpu_times:user+system",
        "clock_ticks_per_second": 1_000_000,
    }


@pytest.mark.parametrize(
    "change",
    [
        {"clock_ticks_per_second": 100},
        {"source": "/proc/<pid>/stat:utime+stime"},
        {"unit": "seconds"},
        {"raw_cpu": {"user_ticks": 5, "system_ticks": 0}},
        {"value": None, "missing_reason": "source_unavailable"},
    ],
)
def test_macos_samples_reject_wrong_scale_source_or_components(change):
    with pytest.raises(ContractError):
        Document.parse("sample", {**macos_cpu(), **change})


@pytest.mark.parametrize("observer", ["common_observer", "boundary_observer"])
def test_overhead_replay_binds_saved_platform_not_reading_host(observer):
    linux = protocol(**{observer: True})
    mac = protocol(**{observer: True}, resource_collector="macos-resource.v1")
    validate_protocol(linux)
    validate_protocol(mac)
    assert linux["protocol_sha256"] != mac["protocol_sha256"]
    assert "resource_collector" not in linux
    forged = deepcopy(mac)
    forged.pop("resource_collector")
    with pytest.raises(EvidenceError, match="protocol_mismatch"):
        validate_protocol(forged)


def test_macos_idle_rss_reduces_on_any_host_and_rejects_changed_source():
    rows, samples, events = idle_inputs()
    for sample in samples:
        sample["source"] = MACOS_SOURCE
    summary, items = idle_rss_observations(
        RUN, "w1", ["c"], rows, samples, events, CONFIG, EVIDENCE, complete=True
    )
    assert summary["first_cycle_rss_bytes"] == 1000 and items[-1]["value"] == -100
    assert all(item["source"] == MACOS_SOURCE for item in items)
    samples[1]["source"] = "/proc/<pid>/status:VmRSS:cycle_confirmed_idle"
    _, items = idle_rss_observations(
        RUN, "w1", ["c"], rows, samples, events, CONFIG, EVIDENCE, complete=True
    )
    assert items[-1]["value"] is None
    assert items[-1]["missing_reason"] == "idle_cycle_source_changed"


@pytest.mark.parametrize("first_cycle", ["missing", "duplicate"])
def test_missing_initial_macos_idle_sample_retains_later_observations(first_cycle):
    rows, samples, events = idle_inputs()
    for sample in samples:
        sample["source"] = MACOS_SOURCE
    samples = samples[1:] if first_cycle == "missing" else [samples[0], *samples]
    _, items = idle_rss_observations(
        RUN, "w1", ["c"], rows, samples, events, CONFIG, EVIDENCE, complete=True
    )
    assert items[-2]["value"] == 900 and items[-2]["source"] == MACOS_SOURCE
    assert items[-1]["value"] is None
    assert items[-1]["missing_reason"] == "first_cycle_baseline_missing"
