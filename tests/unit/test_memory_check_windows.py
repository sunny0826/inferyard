"""Windows inventory and decision regressions use synthetic identities, never live apps."""

import json
import subprocess
import sys
import time
from copy import deepcopy
from pathlib import Path
from types import SimpleNamespace

import psutil
import pytest

from inferyard.application import memory_check
from inferyard.contracts.memory_check import validate_memory_check
from inferyard.contracts.validation import ContractError
from inferyard.platforms import memory_windows as native


class Process:
    def name(self):
        return "editor.exe"

    def exe(self):
        return r"C:\Apps\editor.exe"

    def ppid(self):
        return 1

    def memory_info(self):
        return SimpleNamespace(rss=100, private=300)

    def cmdline(self):
        raise AssertionError("must not read command lines")

    def environ(self):
        raise AssertionError("must not read process environment")


@pytest.fixture
def boundary(monkeypatch):
    monkeypatch.setattr(psutil, "Process", lambda _: Process())
    monkeypatch.setattr(native, "process_start", lambda pid: pid * 100)
    monkeypatch.setattr(native, "owner_session", lambda _: ("private-owner", 1))
    monkeypatch.setattr(
        native,
        "_executable",
        lambda _: {
            "key": "private-path:volume:file",
            "name": "editor.exe",
            "path": r"C:\Apps\editor.exe",
        },
    )


def observed(pid=10, **updates):
    row = {
        "pid": pid,
        "start_identity": pid * 100,
        "parent_pid": 0,
        "parent_start_identity": None,
        "parent_identity_missing_reason": None,
        "name": "editor.exe",
        "owner": "private-owner",
        "session": 1,
        "ownership_missing_reason": None,
        "executable": r"C:\Apps\editor.exe",
        "image": {"key": "private-path:volume:file", "name": "editor.exe"},
        "rss": native.reading(100, native.RSS_SOURCE),
        "private_commit": native.reading(300, native.PRIVATE_SOURCE),
        "missing_reason": None,
    }
    row.update(updates)
    return row


def snapshot(rows, **updates):
    value = {
        "rows": rows,
        "total_pids": len(rows),
        "caller_pid": 900,
        "collector_pid": 901,
        "caller_owner": "private-owner",
        "caller_session": 1,
        "system_root": r"C:\Windows",
        "memory_total": native.reading(10000, "fixture:total"),
        "memory_available": native.reading(5000, "fixture:available"),
        "collection_ns": 1234,
        "truncation_reason": None,
    }
    value.update(updates)
    return value


def report(monkeypatch, rows, protected=(), **updates):
    monkeypatch.setattr(memory_check.platform, "system", lambda: "Windows")
    return memory_check.inspect_memory(protected, collector=lambda **_: snapshot(rows, **updates))


def test_working_set_and_private_commit_are_distinct(boundary):
    row = native.observe(10, psutil)
    assert row["rss"]["value"] == 100
    assert row["private_commit"]["value"] == 300
    assert row["start_identity"] == 1000
    assert row["owner"] == "private-owner"


@pytest.mark.parametrize("field", ["start", "owner", "session"])
def test_identity_reuse_discards_both_measurements(boundary, monkeypatch, field):
    calls = iter([1000, 1001])
    if field == "start":
        monkeypatch.setattr(native, "process_start", lambda pid: next(calls) if pid == 10 else 100)
    else:
        values = iter([("a", 1), ("b", 1) if field == "owner" else ("a", 2)])
        monkeypatch.setattr(native, "owner_session", lambda _: next(values))
    row = native.observe(10, psutil)
    assert row["missing_reason"] == "identity_changed"
    assert row["start_identity"] is None
    assert row["rss"]["value"] is row["private_commit"]["value"] is None
    assert row["image"] is None


@pytest.mark.parametrize(
    "error,reason",
    [(psutil.AccessDenied(10), "access_denied"), (psutil.NoSuchProcess(10), "process_exited")],
)
def test_inaccessible_and_exited_keep_rows(boundary, monkeypatch, error, reason):
    def fail(_):
        raise error

    monkeypatch.setattr(psutil, "Process", fail)
    row = native.observe(10, psutil)
    assert row["missing_reason"] == reason
    assert row["rss"]["value"] is None


def test_owner_missing_preserves_memory_without_group_or_recommendation(boundary, monkeypatch):
    def denied(_):
        raise PermissionError

    monkeypatch.setattr(native, "owner_session", denied)
    row = native.observe(10, psutil)
    data = report(monkeypatch, [row])
    process = data["processes"][0]
    assert process["rss"]["value"] == 100
    assert process["ownership"]["relation"] == "unknown"
    assert process["group_id"] is None
    assert process["decision"]["status"] == "unknown"
    assert data["coverage"]["missing"] == 1


def test_system_pid_zero_is_not_reported_as_exited(boundary, monkeypatch):
    def denied(_):
        error = OSError()
        error.winerror = 87
        raise error

    monkeypatch.setattr(native, "process_start", denied)
    row = native.observe(0, psutil)
    assert row["missing_reason"] == "system_identity_unavailable"
    data = report(monkeypatch, [row])
    assert data["processes"][0]["decision"]["status"] == "protected"


def test_parent_reuse_does_not_discard_own_memory_or_propagate_protection(boundary, monkeypatch):
    parents = iter([100, 101])
    monkeypatch.setattr(native, "process_start", lambda pid: next(parents) if pid == 1 else 1000)
    row = native.observe(10, psutil)
    data = report(monkeypatch, [observed(1), row], protected=(1,))
    assert row["rss"]["value"] == 100
    assert row["parent_start_identity"] is None
    assert data["processes"][1]["decision"]["status"] == "unknown"
    assert not data["coverage"]["complete"]


def test_groups_use_file_owner_session_never_name_and_redact_identity(monkeypatch):
    rows = [
        observed(),
        observed(11),
        observed(12, image={"key": "different-copy"}),
        observed(13, owner="someone-else"),
        observed(14, session=2),
    ]
    data = report(monkeypatch, rows)
    assert len(data["groups"]) == 4
    group = next(g for g in data["groups"] if 10 in g["member_pids"])
    assert group["member_pids"] == [10, 11]
    assert group["rss_total_bytes"] == 200
    assert group["private_commit_total_bytes"] == 600
    assert group["decision"]["status"] == "unknown"
    serialized = json.dumps(data)
    assert "private-owner" not in serialized and "private-path" not in serialized
    assert "C:\\" not in serialized


@pytest.mark.parametrize(
    "updates",
    [
        {"name": "MsMpEng.exe"},
        {"name": "codex.exe"},
        {"name": "lody.exe"},
        {"name": "ssh.exe"},
        {"session": 0},
        {"owner": "other"},
        {"executable": r"C:\Windows\System32\tool.exe"},
        {"executable": r"C:\Users\someone\.lody\agent.exe"},
    ],
)
def test_protections_and_verified_descendants(monkeypatch, updates):
    data = report(
        monkeypatch, [observed(**updates), observed(11, parent_pid=10, parent_start_identity=1000)]
    )
    assert data["processes"][0]["decision"]["status"] == "protected"
    if "Windows" not in updates.get("executable", ""):
        assert data["processes"][1]["decision"]["status"] == "protected"


def test_declared_tree_and_caller_ancestors_do_not_protect_siblings(monkeypatch):
    rows = [
        observed(1),
        observed(10, parent_pid=1, parent_start_identity=100),
        observed(11, parent_pid=10, parent_start_identity=1000),
        observed(12, parent_pid=1, parent_start_identity=100),
    ]
    data = report(monkeypatch, rows, protected=(10, 10), caller_pid=10)
    assert [p["decision"]["status"] for p in data["processes"]] == [
        "protected",
        "protected",
        "protected",
        "unknown",
    ]
    assert len(data["protected_requests"]) == 1
    unavailable = report(monkeypatch, rows, protected=(99,))
    assert not unavailable["coverage"]["complete"]
    assert unavailable["protected_requests"][0]["status"] == "unavailable"


def test_partial_private_measurement_keeps_working_set_and_total_null(monkeypatch):
    data = report(
        monkeypatch,
        [
            observed(
                private_commit=native.reading(
                    None, native.PRIVATE_SOURCE, "private_commit_unavailable"
                )
            ),
            observed(11),
        ],
    )
    group = data["groups"][0]
    assert group["rss_total_bytes"] == 200
    assert group["private_commit_total_bytes"] is None
    assert group["private_commit_known_bytes"] == 300
    assert data["coverage"]["missing"] == 1


def test_agent_ancestor_protects_its_other_descendants(monkeypatch):
    rows = [
        observed(1, name="lody.exe"),
        observed(10, parent_pid=1, parent_start_identity=100),
        observed(11, parent_pid=1, parent_start_identity=100),
    ]
    data = report(monkeypatch, rows, caller_pid=10)
    assert all(p["decision"]["status"] == "protected" for p in data["processes"])


def test_security_name_under_system_root_protects_descendants(monkeypatch):
    data = report(
        monkeypatch,
        [
            observed(name="MsMpEng.exe", executable=r"C:\Windows\security\MsMpEng.exe"),
            observed(11, parent_pid=10, parent_start_identity=1000),
        ],
    )
    assert data["processes"][1]["decision"]["status"] == "protected"


@pytest.mark.parametrize(
    "change",
    [
        lambda d: d.update(kind="memory_check.v1"),
        lambda d: d["coverage"].update(complete=False),
        lambda d: d["processes"][0]["rss"].update(value=True),
        lambda d: d["processes"][0]["private_commit"].update(value=-1),
        lambda d: d["processes"][0]["ownership"].update(relation="unknown"),
        lambda d: d["processes"][0]["decision"].update(status="review"),
        lambda d: d["groups"][0].update(private_commit_total_bytes=100),
        lambda d: d["processes"][0].update(identity_source="fake"),
    ],
)
def test_windows_contract_rejects_corruption(monkeypatch, change):
    data = deepcopy(report(monkeypatch, [observed()]))
    change(data)
    with pytest.raises(ContractError):
        validate_memory_check(data)


def test_deadline_reaps_owned_worker_with_partial_host(tmp_path, monkeypatch):
    marker = tmp_path / "pid.txt"
    host = snapshot([])
    host.pop("rows")
    script = (
        "import os, pathlib, time; "
        f"pathlib.Path({str(marker)!r}).write_text(str(os.getpid())); "
        f"print({json.dumps(('host', host))!r}, flush=True); time.sleep(30)"
    )
    launch = subprocess.Popen
    owned = []

    def record(*args, **kwargs):
        worker = launch(*args, **kwargs)
        owned.append(worker)
        return worker

    monkeypatch.setattr(subprocess, "Popen", record)
    started = time.monotonic()
    value = native.collect(
        deadline_seconds=0.5,
        max_processes=10,
        command=[getattr(sys, "_base_executable", sys.executable), "-c", script],
    )
    assert time.monotonic() - started < 3
    assert value["truncation_reason"] == "deadline"
    assert len(owned) == 1 and owned[0].pid == int(marker.read_text())
    # Verify the exact owned handle exited; numeric PID visibility/reuse is not identity.
    assert owned[0].poll() is not None


@pytest.mark.parametrize("count", [1, 2, 3])
def test_worker_process_limit(boundary, monkeypatch, count):
    messages = []
    monkeypatch.setattr(psutil, "pids", lambda: list(range(10, 10 + count)))
    monkeypatch.setattr(psutil, "virtual_memory", lambda: SimpleNamespace(total=100, available=50))
    monkeypatch.setattr(native, "_send", lambda *message: messages.append(message))
    native._worker(2)
    assert messages[0][1]["total_pids"] == count
    assert len([m for m in messages if m[0] == "process"]) == min(count, 2)
    assert messages[-1] == ("done", "process_limit" if count > 2 else None)


def test_failed_worker_is_tool_error():
    with pytest.raises(OSError, match="memory_collector_failed"):
        native.collect(
            deadline_seconds=3,
            max_processes=1,
            command=[sys.executable, "-c", "raise SystemExit(4)"],
        )


def test_windows_saved_contract_examples():
    from inferyard.contracts.memory_check import read_memory_check

    root = Path(__file__).parents[1] / "fixtures/contracts"
    valid = read_memory_check(root / "memory-check.windows.valid.json")
    assert valid["platform"] == "Windows"
    with pytest.raises(ContractError):
        read_memory_check(root / "memory-check.windows.invalid.json")


def test_cancellation_reaps_only_owned_worker(monkeypatch):
    class Worker:
        stdout = SimpleNamespace(close=lambda: None)
        killed = False

        def communicate(self, **_):
            raise KeyboardInterrupt

        def poll(self):
            return 0 if self.killed else None

        def kill(self):
            self.killed = True

        def wait(self, **_):
            return 0

    worker = Worker()
    monkeypatch.setattr(subprocess, "Popen", lambda *a, **k: worker)
    with pytest.raises(KeyboardInterrupt):
        native.collect(deadline_seconds=1, max_processes=1)
    assert worker.killed


def test_worker_command_owns_actual_interpreter_and_preserves_module_imports(tmp_path):
    marker = tmp_path / "worker.py"
    marker.write_text("import os, sys; print(os.getpid()); print(sys.argv[-1])", encoding="utf-8")
    command = native._worker_command(7)
    command[-1] = command[-1].replace(
        "runpy.run_module('inferyard.platforms.memory_windows', run_name='__main__')",
        f"runpy.run_path({str(marker)!r}, run_name='__main__')",
    )
    with subprocess.Popen(command, stdout=subprocess.PIPE) as worker:
        output, _ = worker.communicate(timeout=3)
        worker_pid, limit = output.decode().splitlines()
        assert int(worker_pid) == worker.pid
        assert limit == "7" and worker.returncode == 0
