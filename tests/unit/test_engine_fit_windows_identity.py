"""Synthetic Windows native failure paths, never a real model or FILETIME API."""

import hashlib
import socket
from types import SimpleNamespace

import pytest

from inferyard.config.engine_fit_native_sources import WINDOWS_SCOPE, WINDOWS_START_SOURCE
from inferyard.platforms import engine_fit_windows as native
from inferyard.platforms import engine_fit_windows_resources as resources
from inferyard.platforms.identity import PreflightError

TICKS = 134_090_000_000_000_123


@pytest.fixture
def windows(tmp_path, monkeypatch):
    executable = tmp_path / "LLAMA-SERVER.exe"
    executable.write_bytes(b"synthetic executable, not a Windows image")
    model = tmp_path / "model.gguf"
    model.write_bytes(b"synthetic test data, not model weights")
    box = {
        "start": {321: TICKS, 654: TICKS + 1},
        "args": [str(executable), "--model", "model.gguf", "--metrics"],
        "exe": str(executable),
        "cwd": str(tmp_path),
        "owner": "test-account",
        "environment": {},
        "hook": lambda: None,
        "rows": {
            321: {"parent": 1, "cpu": 3.5, "rss": 1024, "children": [654]},
            654: {"parent": 321, "cpu": 0.5, "rss": 2048, "children": []},
        },
        "memory": 2**30,
    }

    class Process:
        def __init__(self, pid=None):
            self.pid = pid

        def username(self):
            return "test-account" if self.pid is None else box["owner"]

        def cmdline(self):
            return list(box["args"])

        def exe(self):
            return box["exe"]

        def cwd(self):
            return box["cwd"]

        def environ(self):
            return dict(box["environment"])

        def ppid(self):
            return box["rows"][self.pid]["parent"]

        def cpu_times(self):
            return SimpleNamespace(user=box["rows"][self.pid]["cpu"], system=0.0)

        def memory_info(self):
            return SimpleNamespace(rss=box["rows"][self.pid]["rss"])

        def children(self, *, recursive):
            assert recursive is False
            return [SimpleNamespace(pid=pid) for pid in box["rows"][self.pid]["children"]]

        def status(self):
            return "running"

    def listener(pid, address, port):
        box["hook"]()
        return f"windows:tcp:{address}:{port}:pid:{pid}"

    monkeypatch.setattr(native.psutil, "Process", Process)
    monkeypatch.setattr(native, "_start", lambda pid: box["start"][pid])
    monkeypatch.setattr(resources, "_start", lambda pid: box["start"][pid])
    monkeypatch.setattr(native, "_listener", listener)
    monkeypatch.setattr(
        resources.psutil, "virtual_memory", lambda: SimpleNamespace(available=box["memory"])
    )
    return box, model


def bind(windows):
    return native.bind_service("llama-cpp", windows[1], 321, "http://127.0.0.1:8080")


def test_binding_preserves_exact_filetime_and_no_private_values(windows):
    box, model = windows
    box["args"] += ["--api-key", "synthetic-private-key"]
    value = bind(windows)
    assert value["start_ticks"] == TICKS
    assert value["process_start_source"] == WINDOWS_START_SOURCE
    assert value["listener_inode"] is None
    assert value["listener_identity"] == "windows:tcp:127.0.0.1:8080:pid:321"
    assert value["model_binding"]["path"] == str(model)
    assert (
        value["executable_sha256"]
        == hashlib.sha256(model.with_name("LLAMA-SERVER.exe").read_bytes()).hexdigest()
    )
    assert "test-account" not in repr(value) and "synthetic-private-key" not in repr(value)


@pytest.mark.parametrize("key", ["LLAMA_ARG_RPC", "llama_arg_model", "LlAmA_ArG_MMProj"])
def test_windows_environment_cannot_bypass_asset_binding(windows, key):
    windows[0]["environment"][key] = "must-not-persist"
    with pytest.raises(PreflightError, match="environment_requires_explicit_args"):
        bind(windows)


@pytest.mark.parametrize("owner", ["other-account", "", None, 1])
def test_service_account_must_be_readable_and_match(windows, owner):
    windows[0]["owner"] = owner
    with pytest.raises(PreflightError, match="user_mismatch"):
        bind(windows)


@pytest.mark.parametrize("change", ["start", "args", "owner", "exe", "model"])
def test_identity_change_between_native_observations_is_rejected(windows, change):
    box, model = windows

    def changed():
        if change == "start":
            box["start"][321] += 1
        elif change == "args":
            box["args"] += ["--no-warmup"]
        elif change == "owner":
            box["owner"] = "another-account"
        elif change == "exe":
            model.with_name("LLAMA-SERVER.exe").write_bytes(b"changed")
        else:
            model.unlink()
            model.write_bytes(b"replacement")

    box["hook"] = changed
    with pytest.raises(PreflightError):
        bind(windows)


def test_windows_cannot_accept_a_ninfer_or_draft_entrypoint(windows):
    for engine in ("ninfer", "lmstudio", "vllm"):
        with pytest.raises(PreflightError, match="windows_engine_unavailable"):
            native.bind_service(engine, windows[1], 321, "http://127.0.0.1:8080")
    windows[0]["args"] += ["--model-draft", "other.gguf"]
    with pytest.raises(PreflightError, match="argument_ambiguous"):
        bind(windows)


def test_resources_sum_complete_tree_with_windows_units(windows):
    result = resources.resource_snapshot({"pid": 321, "start_ticks": TICKS})
    assert result["process_tree_rss_bytes"] == 3072
    assert result["process_tree_cpu_seconds"] == 4.0
    assert result["process_count"] == 2
    assert result["scope"] == WINDOWS_SCOPE and result["missing_reasons"] == {}


def test_cpu_counter_regression_during_tree_recheck_is_missing(windows, monkeypatch):
    original, calls = resources._row, {}

    def regressed(pid, deadline):
        row = original(pid, deadline)
        calls[pid] = calls.get(pid, 0) + 1
        if calls[pid] == 2:
            row["cpu"] = 0.0
        return row

    monkeypatch.setattr(resources, "_row", regressed)
    result = resources.resource_snapshot({"pid": 321, "start_ticks": TICKS})
    assert result["process_tree_cpu_seconds"] is None
    assert result["process_tree_rss_bytes"] is None
    assert result["missing_reasons"]["process_count"] == "engine_fit_process_counter_unavailable"


@pytest.mark.parametrize("value", [True, 0, -1, float(TICKS), 2**63])
def test_creation_identity_requires_exact_supported_integer(monkeypatch, value):
    from inferyard.platforms import windows_api

    monkeypatch.setattr(windows_api, "process_start", lambda _: value)
    with pytest.raises(PreflightError, match="identity_unavailable"):
        native._start(321)


@pytest.mark.parametrize("change", ["parent", "nan", "missing", "duplicate", "root_reused"])
def test_unstable_or_missing_member_cannot_become_partial_sum(windows, change, monkeypatch):
    box, _ = windows
    if change == "parent":
        box["rows"][654]["parent"] = 999
    elif change == "nan":
        box["rows"][654]["cpu"] = float("nan")
    elif change == "missing":
        monkeypatch.setattr(resources, "_start", lambda pid: (_ for _ in ()).throw(OSError()))
    elif change == "duplicate":
        box["rows"][321]["children"] = [654, 654]
    else:
        box["start"][321] += 1
    result = resources.resource_snapshot({"pid": 321, "start_ticks": TICKS})
    for name in ("process_tree_rss_bytes", "process_tree_cpu_seconds", "process_count"):
        assert result[name] is None and result["missing_reasons"][name]
    assert result["memory_available_bytes"] == 2**30


@pytest.mark.parametrize("extra", ["same_pid", "other_pid", "ipv6_wildcard"])
def test_listener_ambiguity_rejected_without_reading_other_process_details(monkeypatch, extra):
    # Call the real selection function, independently of the synthetic bind fixture.
    row = SimpleNamespace(
        family=socket.AF_INET,
        pid=321,
        status=native.psutil.CONN_LISTEN,
        laddr=SimpleNamespace(ip="127.0.0.1", port=8080),
    )
    other = SimpleNamespace(
        family=socket.AF_INET6 if extra == "ipv6_wildcard" else socket.AF_INET,
        pid=321 if extra == "same_pid" else 654,
        status=native.psutil.CONN_LISTEN,
        laddr=SimpleNamespace(ip="::" if extra == "ipv6_wildcard" else "127.0.0.1", port=8080),
    )
    monkeypatch.setattr(native.psutil, "net_connections", lambda **_: [row, other])
    with pytest.raises(PreflightError, match="endpoint_pid_mismatch"):
        native._listener(321, "127.0.0.1", 8080)
