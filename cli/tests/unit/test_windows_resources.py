"""Windows batch memory selection, source identity and explicit missing values."""

from types import SimpleNamespace

import pytest

from inferyard.contracts.validation import validate_document
from inferyard.platforms import resources, resources_windows
from inferyard.platforms.identity import PreflightError
from tests.unit.test_resources import CONFIG, envelope


class PsutilError(Exception):
    pass


class AccessDenied(PsutilError):
    pass


class NoSuchProcess(PsutilError):
    pass


psutil = SimpleNamespace(Error=PsutilError, AccessDenied=AccessDenied, NoSuchProcess=NoSuchProcess)


@pytest.fixture
def sampler(monkeypatch):
    monkeypatch.setattr(resources_windows, "psutil_module", lambda: psutil)
    monkeypatch.setattr(resources_windows, "process_start_ticks", lambda pid: 55)
    monkeypatch.setattr(
        psutil,
        "Process",
        lambda pid: SimpleNamespace(memory_info=lambda: SimpleNamespace(rss=8192)),
        raising=False,
    )
    monkeypatch.setattr(
        psutil, "virtual_memory", lambda: SimpleNamespace(available=16384), raising=False
    )
    snapshots = {}
    store = SimpleNamespace(
        observe=lambda kind, value, *, stage, key=None: snapshots.__setitem__((kind, stage), value)
    )
    value = resources_windows.ResourceSampler(store, CONFIG)
    value.set_phase("formal", "request-1")
    return value, snapshots


def test_unknown_platform_remains_closed(monkeypatch):
    monkeypatch.setattr(resources, "sys", SimpleNamespace(platform="unsupported"))
    with pytest.raises(PreflightError, match="resource_collector_platform_unsupported"):
        resources.resource_collector_id()


def test_memory_sources_units_identity_and_missing_metadata(sampler):
    value, snapshots = sampler
    rows = value.collect(77, 55)
    assert [r["value"] for r in rows] == [16384, 8192]
    assert rows[0]["server_pid"] is rows[0]["process_start_ticks"] is None
    assert rows[1]["server_pid"] == 77 and rows[1]["process_start_ticks"] == 55
    for row in rows:
        assert row["source"] == resources_windows.SOURCES[row["metric_name"]]
        assert row["unit"] == "bytes" and row["missing_reason"] is None
        assert row["read_finished_ns"] >= row["read_started_ns"]
        validate_document("sample", envelope(row))
    metadata = snapshots[("collector", "prepare")]
    assert metadata["collector"] == "windows-resource.v1"
    assert metadata["sensors"]["sources"] == []
    assert all(
        row["value"] is None and row["missing_reason"]
        for row in metadata["unavailable_metrics"].values()
    )


@pytest.mark.parametrize("changed_at", ["before", "after"])
def test_pid_reuse_stays_missing_and_host_memory_survives(sampler, monkeypatch, changed_at):
    value, _ = sampler
    starts = iter([56] if changed_at == "before" else [55, 56])
    monkeypatch.setattr(resources_windows, "process_start_ticks", lambda pid: next(starts))
    assert value.collect(77, 55)[1]["missing_reason"] == "source_changed"
    monkeypatch.setattr(resources_windows, "process_start_ticks", lambda pid: 55)
    rows = value.collect(77, 55)
    assert rows[1]["value"] is None and rows[1]["missing_reason"] == "source_changed"
    assert rows[0]["value"] == 16384


@pytest.mark.parametrize("metric", ["service_rss", "system_mem_available"])
@pytest.mark.parametrize("bad", [True, -1, 1.5, float("nan"), float("inf"), None])
def test_invalid_values_are_null_with_reason(sampler, monkeypatch, metric, bad):
    value, _ = sampler
    if metric == "service_rss":
        monkeypatch.setattr(
            psutil,
            "Process",
            lambda pid: SimpleNamespace(memory_info=lambda: SimpleNamespace(rss=bad)),
        )
    else:
        monkeypatch.setattr(psutil, "virtual_memory", lambda: SimpleNamespace(available=bad))
    row = next(r for r in value.collect(77, 55) if r["metric_name"] == metric)
    assert row["value"] is None and row["missing_reason"] == "invalid_memory_value"
    validate_document("sample", envelope(row))


@pytest.mark.parametrize(
    "error,reason",
    [(psutil.AccessDenied(77), "permission_denied"), (OSError(), "source_unavailable")],
)
@pytest.mark.parametrize("metric", ["service_rss", "system_mem_available"])
def test_read_failures_are_explicit(sampler, monkeypatch, error, reason, metric):
    value, _ = sampler

    def fail(*args):
        raise error

    monkeypatch.setattr(psutil, "Process" if metric == "service_rss" else "virtual_memory", fail)
    rows = value.collect(77, 55)
    row = next(r for r in rows if r["metric_name"] == metric)
    assert row["value"] is None and row["missing_reason"] == reason
    assert next(r for r in rows if r["metric_name"] != metric)["value"] is not None


def test_missing_process_cannot_be_replaced_with_same_pid(sampler, monkeypatch):
    value, _ = sampler

    def gone(pid):
        raise psutil.NoSuchProcess(pid)

    monkeypatch.setattr(psutil, "Process", gone)
    assert value.collect(77, 55)[1]["missing_reason"] == "source_unavailable"
    assert value.collect(77, 55)[1]["missing_reason"] == "source_changed"
