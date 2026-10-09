"""Darwin counter semantics and saved-source replay, without a model service."""

import sys
from copy import deepcopy
from types import ModuleType, SimpleNamespace
from unittest.mock import Mock

import pytest

from inferyard.evidence.storage import EvidenceError
from inferyard.platforms import external_cpu, resources, resources_macos
from inferyard.platforms.identity import PreflightError
from inferyard.runtime.boundary_observer import boundary_contract
from inferyard.runtime.environment_observer import observer_contract

ENDPOINT = {"server_pid": 77, "process_start_ticks": 55}
CONFIG = {"endpoint": ENDPOINT, "telemetry": {"interval_ms": 500}}


class UnavailableSensors:
    """No unsupported privileged measurement is replaced with a fabricated zero."""

    sources = ()

    def metadata(self):
        return {
            "sources": [],
            "discovery_issues": [{"reason": "macos_unprivileged_sensors_unavailable"}],
            "limitations": ["temperature_frequency_energy_not_collected"],
            "missing_metrics": ["temperature", "frequency"],
        }

    def collect(self, phase, request_id):
        return []


@pytest.fixture
def native(monkeypatch):
    """The native API boundary is mocked, not a fabricated /proc on Darwin."""
    monkeypatch.setattr(sys, "platform", "darwin")
    from inferyard.platforms import sensors_macos

    monkeypatch.setattr(resources_macos, "MacSensors", UnavailableSensors)
    monkeypatch.setattr(sensors_macos, "MacSensors", UnavailableSensors)
    native = ModuleType("inferyard.platforms.macos_native")
    identity = ModuleType("inferyard.platforms.macos_identity")
    state = SimpleNamespace(
        boot="boot-mac",
        start=55,
        user=1.25,
        system=0.5,
        host=[1.0, 0.25, 0.5, 4.0],
        page_size=16384,
        swap_in=3,
        swap_out=4,
    )

    class PsutilError(Exception):
        pass

    class AccessDenied(PsutilError):
        pass

    process = SimpleNamespace(
        cpu_times=lambda: SimpleNamespace(user=state.user, system=state.system)
    )
    state.psutil = SimpleNamespace(
        Process=lambda pid: process,
        Error=PsutilError,
        AccessDenied=AccessDenied,
        cpu_times=lambda: SimpleNamespace(
            **dict(zip(("user", "nice", "system", "idle"), state.host, strict=True))
        ),
    )
    identity.process_start_ticks = lambda pid: state.start
    native.psutil_module = lambda: state.psutil
    native.boot_id = lambda: state.boot
    native.swap_snapshot = lambda: {
        "pswpin": state.swap_in,
        "pswpout": state.swap_out,
        "page_size_bytes": state.page_size,
        "source": {
            "pswpin": resources_macos.SOURCES["system_swap_in"],
            "pswpout": resources_macos.SOURCES["system_swap_out"],
        },
    }
    monkeypatch.setitem(sys.modules, native.__name__, native)
    monkeypatch.setitem(sys.modules, identity.__name__, identity)
    state.native, state.identity = native, identity
    return state


def sampler():
    snapshots, samples, observations = {}, [], []
    store = SimpleNamespace(
        snapshot=lambda name, value: snapshots.__setitem__(name, value),
        sample=samples.append,
        observation=lambda *row: observations.append(row),
    )
    value = resources.ResourceSampler(store, CONFIG, page_size=16384)
    return value, snapshots, samples, observations


def test_factory_and_native_counter_units(native):
    value, snapshots, _, _ = sampler()
    assert resources.resource_collector_id() == "macos-resource.v1"
    assert isinstance(value, resources_macos.ResourceSampler)
    rows = value.counters(77, 55)
    assert [row["value"] for row in rows] == [1_750_000, 3, 4]
    assert rows[0]["raw_cpu"] == {"user_ticks": 1_250_000, "system_ticks": 500_000}
    assert rows[0]["clock_ticks_per_second"] == 1_000_000
    assert [row["page_size_bytes"] for row in rows[1:]] == [16384, 16384]
    assert all(row["boot_id"] == "boot-mac" for row in rows)
    assert snapshots["collector.json"]["collector"] == "macos-resource.v1"
    assert snapshots["collector.json"]["sensors"]["missing_metrics"] == ["temperature", "frequency"]
    assert snapshots["observer.json"]["kind"] == "macos-environment-observer.v1"
    assert snapshots["boundary-observer.json"]["collector"] == "macos-resource.v1"


def test_explicit_proc_root_retains_linux_factory(native, tmp_path):
    from inferyard.platforms.resources_linux import ResourceSampler
    from tests.unit.test_resources import proc

    proc(tmp_path)
    store = SimpleNamespace(snapshot=lambda *args: None)
    value = resources.ResourceSampler(store, CONFIG, proc_root=tmp_path, ticks_per_second=100)
    assert isinstance(value, ResourceSampler)
    assert value.counters(77, 55)[0]["value"] == 30


def test_cpu_checks_process_identity_after_read_and_stays_missing(native):
    value, _, _, _ = sampler()
    identities = iter([55, 56])
    native.identity.process_start_ticks = lambda pid: next(identities)
    row = value.counters(77, 55)[0]
    assert row["value"] is None and row["missing_reason"] == "source_changed"
    native.identity.process_start_ticks = lambda pid: 55
    rows = value.counters(77, 55)
    assert rows[0]["missing_reason"] == "source_changed"
    assert rows[1]["value"] == 3


@pytest.mark.parametrize("bad", [-1, float("nan"), float("inf"), True])
def test_invalid_native_cpu_is_missing_not_zero(native, bad):
    value, _, _, _ = sampler()
    native.user = bad
    row = value.counters(77, 55)[0]
    assert row["value"] is None and row["missing_reason"] == "invalid_counter"


def test_counter_permission_boot_and_scale_failures_are_explicit(native):
    value, _, _, _ = sampler()

    def denied(pid):
        raise native.psutil.AccessDenied()

    native.psutil.Process = denied
    assert value.counters(77, 55)[0]["missing_reason"] == "permission_denied"
    native.page_size = 4096
    assert value.counters(77, 55)[1]["missing_reason"] == "counter_scale_changed"
    native.page_size, native.swap_in = 16384, None
    row = value.counters(77, 55)[1]
    assert row["value"] is None and row["missing_reason"] == "source_unavailable"
    native.boot = "new-boot"
    assert all(row["value"] is None for row in value.counters(77, 55))


def test_native_boot_is_checked_after_counter_read(native):
    value, _, _, _ = sampler()
    boot_calls = iter(["boot-mac", "changed", "changed", "changed"])
    native.native.boot_id = lambda: next(boot_calls)
    rows = value.counters(77, 55)
    assert all(row["missing_reason"] == "source_changed" for row in rows)


def test_boundary_records_native_counter_and_idle_rss_sources(native, monkeypatch):
    value, _, samples, observations = sampler()
    monkeypatch.setattr(value.boundary_guard, "observe", lambda *args: None)
    monkeypatch.setattr(resources_macos, "read_rss", lambda *args: (2048, None))
    value.boundary("request_end")
    value.idle_cycle_rss()
    assert samples[0]["capture"] == "request_end"
    assert samples[-1]["source"] == "psutil:Process.memory_info:rss:cycle_confirmed_idle"
    assert observations[0][1]["source"] == external_cpu.MACOS_SOURCE


def native_external(t, host, service):
    return {
        "source": external_cpu.MACOS_SOURCE,
        **ENDPOINT,
        "boot_id": "boot-mac",
        "clock_ticks_per_second": 1_000_000,
        "phase": "formal",
        "request_id": "request-1",
        "read_started_ns": t,
        "read_finished_ns": t,
        "host_ticks": host,
        "service_ticks": service,
        "missing_reason": None,
    }


def native_external_rows():
    return [
        native_external(0, [100, 0, 20, 100], 10),
        native_external(1_000_000_000, [150, 0, 40, 230], 40),
    ]


def test_capture_native_cpu_retains_four_distinct_host_counters(native):
    row = external_cpu.capture(77, 55, "boot-mac", 1_000_000, "formal", "r", lambda: 10)
    assert row["host_ticks"] == [1_000_000, 250_000, 500_000, 4_000_000]
    assert row["service_ticks"] == 1_750_000
    native.start = 56
    missing = external_cpu.capture(77, 55, "boot-mac", 1_000_000, "formal", "r", lambda: 11)
    assert missing["service_ticks"] is None and missing["missing_reason"] == "source_changed"


def test_reduction_uses_saved_source_not_reading_platform(monkeypatch):
    from tests.unit.test_external_cpu import rows as linux_rows

    for platform in ("linux", "darwin", "win32"):
        monkeypatch.setattr(sys, "platform", platform)
        result = external_cpu.reduce_external_cpu(native_external_rows(), ENDPOINT, 1000)
        assert result["observed_max_percent"] == 20
        assert result["source"] == external_cpu.MACOS_SOURCE
        assert (
            external_cpu.reduce_external_cpu(linux_rows(), ENDPOINT, 1000)["observed_max_percent"]
            == 20
        )


@pytest.mark.parametrize(
    "change,reason",
    [
        ("counter", "counter_regression"),
        ("skew", "host_process_counter_skew"),
        ("boot", "counter_identity_changed"),
        ("gap", "sampling_gap"),
    ],
)
def test_native_bad_cpu_intervals_stay_missing(change, reason):
    rows = native_external_rows()
    if change == "counter":
        rows[1]["host_ticks"][0] = 99
    elif change == "skew":
        rows[1]["service_ticks"] = 1000
    elif change == "boot":
        rows[1]["boot_id"] = "other"
    else:
        rows[1].update(read_started_ns=3_000_000_000, read_finished_ns=3_000_000_000)
    result = external_cpu.reduce_external_cpu(rows, ENDPOINT, 1000)
    assert result["observed_max_percent"] is None
    assert result["intervals"][-1]["missing_reason"] == reason


@pytest.mark.parametrize("change", ["source", "shape", "scale"])
def test_mixed_source_or_wrong_scale_cannot_be_reinterpreted(change):
    rows = native_external_rows()
    if change == "source":
        rows[1]["source"] = external_cpu.SOURCE
    elif change == "shape":
        rows[1]["host_ticks"] += [0, 0, 0, 0]
    else:
        rows[1]["clock_ticks_per_second"] = 100
    with pytest.raises(EvidenceError):
        external_cpu.reduce_external_cpu(rows, ENDPOINT, 1000)


def test_observer_contracts_can_replay_linux_on_darwin(native):
    assert (
        observer_contract(500, collector="linux-resource.v2")["kind"]
        == "linux-environment-observer.v1"
    )
    assert (
        boundary_contract(collector="linux-resource.v2")["kind"] == "outside-request-environment.v1"
    )
    assert observer_contract(500) == observer_contract(500, collector="linux-resource.v2")
    assert boundary_contract() == boundary_contract(collector="linux-resource.v2")
    assert observer_contract(500, collector="macos-resource.v1")["collector"] == "macos-resource.v1"


def test_missing_temperature_blocks_native_safety_guard(native):
    from inferyard.runtime.safety import SafetyGuard

    policy = {
        "max_temperature_celsius": 90,
        "require_temperature": True,
        "check_environment": False,
        "max_external_cpu_percent": None,
    }
    guard = SafetyGuard(policy, CONFIG, lambda: {})
    with pytest.raises(PreflightError, match="temperature_safety_source_unavailable"):
        guard.check()
    assert guard.last["temperature_samples"] == []


def test_guardian_uses_native_boot_and_spawn(native, monkeypatch, tmp_path):
    from inferyard.extensions import independent_guard

    context = Mock()
    get_context = Mock(return_value=context)
    monkeypatch.setattr(independent_guard.mp, "get_context", get_context)
    guard = independent_guard.IndependentGuard(tmp_path / "guard.jsonl", CONFIG)
    get_context.assert_called_once_with("spawn")
    assert guard.clock_id == "boot-mac:CLOCK_MONOTONIC"
    assert guard.policy["require_temperature"] is True


def test_resource_windows_use_saved_native_identity(monkeypatch):
    from inferyard.analysis.resource_comparison import MACOS_SOURCES, window
    from tests.unit.test_resource_comparison import POLICY, fixture, refresh

    data = deepcopy(fixture()[0])
    data["resource_collector"].update(
        collector="macos-resource.v1",
        clock_ticks_per_second=1_000_000,
        swap_scope="host_swap_excludes_compression_not_model_attribution",
    )
    for sample in data["samples"]:
        if sample["metric_name"] in MACOS_SOURCES:
            sample.update(
                source=MACOS_SOURCES[sample["metric_name"]], collector="macos-resource.v1"
            )
            if sample["metric_name"] == "service_cpu_ticks":
                sample["clock_ticks_per_second"] = 1_000_000
    refresh(data)
    monkeypatch.setattr(sys, "platform", "linux")
    metric = next(m for m in data["summary"]["metric_observations"] if m["metric_id"] == "C04")
    assert window(data, data["requests"][0], metric, POLICY)["reasons"] == []
    data["resource_collector"]["collector"] = "linux-resource.v2"
    reasons = window(data, data["requests"][0], metric, POLICY)["reasons"]
    assert "resource_source_unknown" in reasons
    assert "resource_counter_collector_binding_mismatch" in reasons


def test_swap_pair_uses_one_fresh_snapshot_and_shared_interval(native):
    value, _, _, _ = sampler()
    clock = iter(range(100, 1000, 100))
    value.clock = lambda: next(clock)
    native.native.swap_snapshot = Mock(wraps=native.native.swap_snapshot)
    first = value.counters(77, 55)
    assert native.native.swap_snapshot.call_count == 1
    assert [(r["read_started_ns"], r["read_finished_ns"]) for r in first] == [
        (100, 200),
        (300, 400),
        (300, 400),
    ]
    native.swap_in, native.swap_out = 10, 20
    second = value.counters(77, 55, capture="request_end")
    assert native.native.swap_snapshot.call_count == 2
    assert [r["value"] for r in second[1:]] == [10, 20]
    assert all(r["capture"] == "request_end" for r in second)
    assert all(r["server_pid"] is None and r["raw_cpu"] is None for r in second[1:])


@pytest.mark.parametrize("bad", [None, -1, True, 1.5, float("nan"), float("inf")])
def test_bad_swap_field_does_not_hide_other_field(native, bad):
    value, _, _, _ = sampler()
    native.swap_in = bad
    rows = value.counters(77, 55)
    assert rows[1]["value"] is None
    assert rows[1]["missing_reason"] == ("source_unavailable" if bad is None else "invalid_counter")
    assert rows[2]["value"] == 4 and rows[2]["missing_reason"] is None
    assert rows[0]["value"] == 1_750_000


@pytest.mark.parametrize("failed_key", ["pswpin", "pswpout"])
def test_wrong_swap_source_is_rejected_per_field(native, failed_key):
    value, _, _, _ = sampler()
    snapshot = native.native.swap_snapshot()
    snapshot["source"][failed_key] = "vm_stat:Pageins"
    native.native.swap_snapshot = lambda: snapshot
    rows = value.counters(77, 55)[1:]
    bad_index = 0 if failed_key == "pswpin" else 1
    assert rows[bad_index]["missing_reason"] == "source_changed"
    assert rows[1 - bad_index]["missing_reason"] is None


@pytest.mark.parametrize("failure", ["boot", "scale", "permission"])
def test_shared_swap_identity_failure_marks_both_missing(native, failure):
    value, _, _, _ = sampler()
    snapshot = native.native.swap_snapshot()

    def changed():
        if failure == "permission":
            raise PermissionError("denied")
        if failure == "boot":
            native.boot = "changed-after-read"
        else:
            snapshot["page_size_bytes"] = 4096
        return snapshot

    native.native.swap_snapshot = changed
    rows = value.counters(77, 55)
    expected = {
        "boot": "source_changed",
        "scale": "counter_scale_changed",
        "permission": "permission_denied",
    }[failure]
    assert rows[0]["missing_reason"] is None
    assert all(r["value"] is None and r["missing_reason"] == expected for r in rows[1:])
    assert rows[1]["read_started_ns"] == rows[2]["read_started_ns"]
    assert rows[1]["read_finished_ns"] == rows[2]["read_finished_ns"]
