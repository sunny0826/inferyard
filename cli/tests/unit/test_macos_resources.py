"""Darwin counter semantics and saved-source replay, without a model service."""

import sys
from copy import deepcopy
from types import ModuleType, SimpleNamespace

import pytest

from inferyard.errors import EvidenceError, PreflightError
from inferyard.platforms import external_cpu, resources, resources_macos

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
        observe=lambda kind, value, *, stage, key=None: snapshots.__setitem__((kind, stage), value),
        sample=samples.append,
        observation=lambda *row: observations.append(row),
    )
    value = resources.ResourceSampler(store, CONFIG, page_size=16384)
    return value, snapshots, samples, observations


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
