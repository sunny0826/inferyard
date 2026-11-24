"""Windows batch memory selection, source identity and explicit missing values."""

from types import SimpleNamespace

import pytest

from inferyard.contracts.validation import validate_document
from inferyard.platforms import resources_windows
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
