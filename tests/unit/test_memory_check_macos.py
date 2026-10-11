"""Native-boundary faults and collector cleanup use only owned test processes."""

import json
import plistlib
import subprocess
import sys
import time
from types import SimpleNamespace

import psutil
import pytest

import inferyard.platforms.memory_macos as native


def test_bundle_identity_uses_outer_app_and_distinguishes_installations(tmp_path):
    paths = []
    for parent in (tmp_path / "one", tmp_path / "two"):
        root = parent / "Browser.app"
        executable = root / "Contents/Helpers/Helper.app/Contents/MacOS/helper"
        executable.parent.mkdir(parents=True)
        executable.write_text("fixture")
        (root / "Contents/Info.plist").write_bytes(plistlib.dumps({"CFBundlePackageType": "APPL"}))
        paths.append(native.bundle_identity(str(executable)))
    assert paths[0]["name"] == paths[1]["name"] == "Browser"
    assert paths[0]["key"] != paths[1]["key"]
    assert native.bundle_identity(str(tmp_path / "missing")) is None


class FakeProcess:
    def ppid(self):
        return 1

    def uids(self):
        return SimpleNamespace(real=501)

    def name(self):
        return "test"

    def exe(self):
        return "/opt/test"

    def memory_info(self):
        return SimpleNamespace(rss=100)

    def cmdline(self):
        raise AssertionError("must not read command line")

    def environ(self):
        raise AssertionError("must not read environment")


def test_observe_checks_identity_without_sensitive_access(monkeypatch):
    monkeypatch.setattr(psutil, "Process", lambda _: FakeProcess())
    monkeypatch.setattr(native, "process_start_ticks", lambda pid: pid * 100)
    value = native.observe(2, psutil)
    assert value["rss"]["value"] == 100
    assert value["missing_reason"] is None


def test_observe_discards_rss_on_pid_reuse(monkeypatch):
    monkeypatch.setattr(psutil, "Process", lambda _: FakeProcess())
    starts = iter([200, 201])
    monkeypatch.setattr(
        native, "process_start_ticks", lambda pid: next(starts) if pid == 2 else 100
    )
    value = native.observe(2, psutil)
    assert value["missing_reason"] == "identity_changed"
    assert value["rss"]["value"] is None
    assert value["start_identity"] is None


@pytest.mark.parametrize(
    "error,reason",
    [
        (psutil.AccessDenied(2), "access_denied"),
        (psutil.NoSuchProcess(2), "process_exited"),
    ],
)
def test_observe_records_query_failures(monkeypatch, error, reason):
    def fail(_):
        raise error

    monkeypatch.setattr(psutil, "Process", fail)
    value = native.observe(2, psutil)
    assert value["missing_reason"] == reason
    assert value["rss"]["missing_reason"] == reason


@pytest.mark.skipif(sys.platform == "win32", reason="POSIX pipe selector transport")
def test_deadline_reaps_only_owned_collector(tmp_path):
    marker = tmp_path / "pid.txt"
    script = (
        "import os, pathlib, time; "
        f"pathlib.Path({str(marker)!r}).write_text(str(os.getpid())); time.sleep(30)"
    )
    started = time.monotonic()
    value = native.collect(
        deadline_seconds=0.2, max_processes=10, command=[sys.executable, "-c", script]
    )
    assert time.monotonic() - started < 3
    assert value["truncation_reason"] == "deadline"
    assert not psutil.pid_exists(int(marker.read_text()))


@pytest.mark.skipif(sys.platform == "win32", reason="POSIX pipe selector transport")
def test_process_limit_reports_unobserved_records():
    host = {
        "total_pids": 3,
        "memory_total": native.reading(100, "fixture:total"),
        "memory_available": native.reading(50, "fixture:available"),
    }
    script = (
        f"print({json.dumps(('host', host))!r}); print({json.dumps(('done', 'process_limit'))!r})"
    )
    value = native.collect(
        deadline_seconds=3, max_processes=1, command=[sys.executable, "-c", script]
    )
    assert value["total_pids"] == 3
    assert value["truncation_reason"] == "process_limit"


@pytest.mark.skipif(sys.platform != "darwin", reason="macOS native diagnostic")
def test_stdin_library_entry_uses_independent_module_worker():
    result = subprocess.run(
        [sys.executable, "-"],
        input="from inferyard.cli import main\nraise SystemExit(main(['memory-check']))",
        text=True,
        capture_output=True,
        timeout=10,
    )
    assert result.returncode in (0, 3), result.stderr
    assert json.loads(result.stdout)["details"]["coverage"]["truncation_reason"] is None


@pytest.mark.parametrize("count", [2, 3, 4])
def test_worker_process_budget_before_at_and_above_limit(monkeypatch, count):
    monkeypatch.setattr(psutil, "pids", lambda: list(range(1, count + 1)))
    monkeypatch.setattr(psutil, "virtual_memory", lambda: SimpleNamespace(total=100, available=50))
    monkeypatch.setattr(native, "observe", lambda pid, _: {"pid": pid})
    messages = []
    connection = SimpleNamespace(send=messages.append, close=lambda: None)
    native._worker(connection, 3)
    assert messages[0][1]["total_pids"] == count
    assert [data["pid"] for tag, data in messages if tag == "process"] == list(
        range(1, min(count, 3) + 1)
    )
    assert messages[-1] == ("done", "process_limit" if count > 3 else None)


def test_owner_change_during_observation_discards_measurement(monkeypatch):
    process = FakeProcess()
    owners = iter([501, 501, 0])
    process.uids = lambda: SimpleNamespace(real=next(owners))
    monkeypatch.setattr(psutil, "Process", lambda _: process)
    monkeypatch.setattr(native, "process_start_ticks", lambda pid: pid * 100)
    value = native.observe(2, psutil)
    assert value["missing_reason"] == "identity_changed"
    assert value["rss"]["value"] is None


@pytest.mark.parametrize("failure_at", [1, 2])
def test_parent_identity_failure_keeps_readable_child_and_unrelated_review(monkeypatch, failure_at):
    from inferyard.application.memory_check import inspect_memory
    from tests.unit.test_memory_check import app, by_pid, observed, snapshot

    calls = 0

    def start(pid):
        nonlocal calls
        if pid == 1:
            calls += 1
            if calls == failure_at:
                raise native.PreflightError("parent_unavailable")
        return pid * 100

    monkeypatch.setattr(psutil, "Process", lambda _: FakeProcess())
    monkeypatch.setattr(native, "process_start_ticks", start)
    child = native.observe(2, psutil)
    assert child["start_identity"] == 200
    assert child["rss"]["value"] == 100
    assert child["missing_reason"] is None
    assert child["parent_start_identity"] is None
    assert child["parent_identity_missing_reason"] == "parent_identity_unavailable"
    data = inspect_memory(
        collector=lambda **_: snapshot([child, observed(10, bundle=app())]), uid=501
    )
    assert data["coverage"]["missing"] == 1
    assert not data["coverage"]["complete"]
    assert by_pid(data, 2)["decision"]["status"] == "unknown"
    assert by_pid(data, 10)["decision"]["status"] == "review"


def test_parent_generation_change_keeps_child_but_never_links_reused_parent(monkeypatch):
    from inferyard.application.memory_check import inspect_memory
    from tests.unit.test_memory_check import app, by_pid, observed, snapshot

    starts = iter([100, 101])
    monkeypatch.setattr(psutil, "Process", lambda _: FakeProcess())
    monkeypatch.setattr(
        native, "process_start_ticks", lambda pid: next(starts) if pid == 1 else 200
    )
    child = native.observe(2, psutil)
    assert child["rss"]["value"] == 100
    assert child["parent_start_identity"] is None
    assert child["parent_identity_missing_reason"] == "parent_identity_changed"
    parent = observed(1, bundle=app("Parent", "parent"))
    data = inspect_memory(
        collector=lambda **_: snapshot([parent, child]), uid=501, protect_pids=[1]
    )
    assert by_pid(data, 2)["group_id"] is None
    assert by_pid(data, 2)["decision"]["status"] == "unknown"
