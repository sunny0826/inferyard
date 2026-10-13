"""Synthetic Darwin identity and complete-tree refusal paths; no model execution."""

import hashlib
import os
from copy import deepcopy
from types import SimpleNamespace

import pytest

from inferyard.platforms import engine_fit as fit
from inferyard.platforms import engine_fit_macos as macos
from inferyard.platforms import engine_fit_macos_listener as listeners
from inferyard.platforms import macos_identity
from inferyard.platforms.identity import PreflightError


class NativeError(Exception):
    pass


class Gone(NativeError):
    pass


class Denied(NativeError):
    pass


@pytest.fixture
def native(tmp_path, monkeypatch):
    model = tmp_path / "model"
    model.mkdir()
    (model / "weights").write_bytes(b"not real model weights")
    executable = tmp_path / "python3.14"
    executable.write_bytes(b"synthetic executable")
    state = {
        "start": 1_700_000_000.125,
        "args": ["python3.14", "-m", "vllm.entrypoints.openai.api_server", "--model", "model"],
        "exe": str(executable),
        "cwd": str(tmp_path),
        "uids": (501, 501, 501),
        "queries": [],
    }
    state["rows"] = [
        {
            "p": 321,
            "f": "txt",
            "t": "REG",
            "D": hex(executable.stat().st_dev),
            "i": str(executable.stat().st_ino),
        },
        {"p": 321, "f": "5", "t": "IPv4", "P": "TCP", "T": "ST=LISTEN", "n": "*:8080"},
    ]
    state["global_rows"] = [dict(state["rows"][1])]
    process = SimpleNamespace(
        _proc=SimpleNamespace(create_time=lambda *, monotonic: state["start"]),
        cmdline=lambda: list(state["args"]),
        exe=lambda: state["exe"],
        cwd=lambda: state["cwd"],
        uids=lambda: state["uids"],
    )
    psutil = SimpleNamespace(
        Process=lambda pid: process,
        Error=NativeError,
        NoSuchProcess=Gone,
        AccessDenied=Denied,
        STATUS_ZOMBIE="zombie",
        STATUS_DEAD="dead",
        virtual_memory=lambda: SimpleNamespace(available=2**30),
    )
    monkeypatch.setattr(macos, "psutil_module", lambda: psutil)
    monkeypatch.setattr(macos_identity, "psutil_module", lambda: psutil)
    monkeypatch.setattr(macos.os, "getuid", lambda: 501, raising=False)
    monkeypatch.setattr(macos.os, "geteuid", lambda: 501, raising=False)

    def view(pid, selectors):
        state["queries"].append((pid, selectors))
        return deepcopy(state["rows"])

    monkeypatch.setattr(macos, "lsof_records", view)

    def all_listeners(command, **kwargs):
        assert command == ["/usr/sbin/lsof", "-nP", "-a", "-iTCP:8080", "-sTCP:LISTEN", "-F0pftPnT"]
        assert kwargs["timeout"] == 2
        output = "".join(
            key + str(value) + "\0" for row in state["global_rows"] for key, value in row.items()
        )
        return SimpleNamespace(returncode=0, stderr="", stdout=output)

    monkeypatch.setattr(listeners.subprocess, "run", all_listeners)
    return state, process, psutil, model


def bind(native, engine="vllm"):
    return macos.bind_service(engine, native[3], 321, "http://127.0.0.1:8080")


@pytest.mark.parametrize(
    "engine,args",
    [
        ("vllm", ["vllm", "serve", "model"]),
        ("vllm", ["python3.14", "/bin/vllm", "serve", "model"]),
        ("vllm", ["python3", "-u", "-m", "vllm.entrypoints.openai.api_server", "--model=model"]),
        ("vllm", ["Python", "-m", "vllm.entrypoints.openai.api_server", "--model=model"]),
        ("sglang", ["python3", "-m", "sglang.launch_server", "--model-path", "model"]),
    ],
)
def test_native_binding_has_raw_start_microseconds_and_actual_sources(native, engine, args):
    state, _, _, model = native
    state["args"] = [*args, "--api-key", "do-not-persist"]
    observed = bind(native, engine)
    assert observed["start_ticks"] == 1_700_000_000_125000
    assert observed["listener_inode"] is None
    assert observed["listener_identity"] == "macos:tcp:127.0.0.1:8080:pid:321"
    assert observed["listener_source"] == "lsof:TCP:LISTEN:pid"
    assert observed["model_binding"]["path"] == str(model.resolve())
    assert observed["cwd_sha256"] == hashlib.sha256(os.fsencode(model.parent)).hexdigest()
    raw = b"\0".join(os.fsencode(value) for value in state["args"]) + b"\0"
    assert observed["argv_sha256"] == hashlib.sha256(raw).hexdigest()
    assert state["queries"] == [(321, [])]
    assert "do-not-persist" not in repr(observed)
    assert "startup_args" not in observed


def test_native_python_framework_executable_name_is_supported(native):
    from pathlib import Path

    state, *_ = native
    executable = Path(state["exe"])
    renamed = executable.with_name("Python")
    executable.rename(renamed)
    state["exe"] = str(renamed)
    assert bind(native)["executable_sha256"] == hashlib.sha256(renamed.read_bytes()).hexdigest()


@pytest.mark.parametrize("pid", [True, 0, -1, 1.0, "321"])
def test_invalid_pid_rejected_before_native_reads(native, pid):
    with pytest.raises(PreflightError, match="invalid_service"):
        macos.bind_service("vllm", native[3], pid, "http://127.0.0.1:8080")
    assert native[0]["queries"] == []


@pytest.mark.parametrize("index", range(3))
def test_service_user_must_match_all_native_uids(native, index):
    state, *_ = native
    ids = list(state["uids"])
    ids[index] += 1
    state["uids"] = tuple(ids)
    with pytest.raises(PreflightError, match="service_user_mismatch"):
        bind(native)


@pytest.mark.parametrize(
    "index,changes,reason",
    [
        (0, {"p": 999}, "mapping_unverified"),
        (0, {"f": "3"}, "mapping_unverified"),
        (0, {"i": "0"}, "mapping_unverified"),
        (0, {"D": "0"}, "mapping_unverified"),
        (0, {"t": "DIR"}, "mapping_unverified"),
        (1, {"p": 999}, "endpoint_pid_mismatch"),
        (1, {"P": "UDP"}, "endpoint_pid_mismatch"),
        (1, {"t": "IPv6"}, "endpoint_pid_mismatch"),
        (1, {"T": "ST=ESTABLISHED"}, "endpoint_pid_mismatch"),
        (1, {"n": "127.0.0.2:8080"}, "endpoint_pid_mismatch"),
        (1, {"n": "127.0.0.1:8081"}, "endpoint_pid_mismatch"),
    ],
)
def test_mapping_and_listener_evidence_cannot_be_substituted(native, index, changes, reason):
    native[0]["rows"][index].update(changes)
    with pytest.raises(PreflightError, match=reason):
        bind(native)


def test_duplicate_listener_is_ambiguous(native):
    state, *_ = native
    state["rows"].append(dict(state["rows"][1], f="6"))
    with pytest.raises(PreflightError, match="endpoint_pid_mismatch"):
        bind(native)


@pytest.mark.parametrize("change", ["other_pid", "same_pid", "ipv6", "other_address", "gone"])
def test_global_reuseport_or_overlapping_listener_refuses_binding(native, change):
    state, *_ = native
    extra = dict(state["global_rows"][0], p=654)
    if change == "same_pid":
        extra.update(p=321, f="6")
    elif change == "ipv6":
        extra["t"] = "IPv6"
    elif change == "other_address":
        extra["n"] = "127.0.0.2:8080"
    if change == "gone":
        state["global_rows"] = []
    else:
        state["global_rows"].append(extra)
    with pytest.raises(PreflightError, match="listener_ambiguous_or_changed"):
        bind(native)


@pytest.mark.parametrize("change", ["start", "args", "cwd", "executable", "model"])
def test_identity_changes_during_lsof_are_rejected(native, monkeypatch, change):
    from pathlib import Path

    state, _, _, model = native
    original = macos.lsof_records

    def changed(*args):
        rows = original(*args)
        if change == "start":
            state["start"] += 1
        elif change == "args":
            state["args"].append("--changed")
        elif change == "cwd":
            state["cwd"] = str(model)
        elif change == "executable":
            Path(state["exe"]).write_bytes(b"changed bytes")
        else:
            model.rename(model.with_name("old-model"))
            model.mkdir()
        return rows

    monkeypatch.setattr(macos, "lsof_records", changed)
    with pytest.raises(PreflightError, match="identity_changed"):
        bind(native)


@pytest.mark.parametrize("read", ["uids", "exe", "cwd", "cmdline"])
def test_native_permission_failure_does_not_emit_partial_binding(native, read):
    process = native[1]

    def denied():
        raise Denied("private error detail")

    setattr(process, read, denied)
    with pytest.raises(PreflightError) as error:
        bind(native)
    assert "private error detail" not in str(error.value)


def test_executable_content_is_bound_without_basename_authority(native, monkeypatch):
    from pathlib import Path

    state, *_ = native
    previous = Path(state["exe"])
    other = previous.with_name("renamed-interpreter")
    previous.rename(other)
    state["exe"] = str(other)
    binding = bind(native)
    assert binding["executable_sha256"] == hashlib.sha256(other.read_bytes()).hexdigest()
    other.write_bytes(b"changed executable")
    monkeypatch.setattr(fit.platform, "system", lambda: "Darwin")
    with pytest.raises(PreflightError, match="identity_changed"):
        fit.check_service(binding)


def test_dispatch_and_later_cwd_change_with_absolute_model_path(native, monkeypatch):
    state, _, _, model = native
    state["args"][-1] = str(model)
    monkeypatch.setattr(fit.platform, "system", lambda: "Darwin")
    observed = fit.bind_service("vllm", model, 321, "http://127.0.0.1:8080")
    fit.check_service(observed)
    state["cwd"] = str(model)
    with pytest.raises(PreflightError, match="identity_changed"):
        fit.check_service(observed)


@pytest.fixture
def tree(native):
    state, _, psutil, _ = native
    nodes = {
        321: {"start": state["start"], "parent": 1, "children": [654]},
        654: {"start": state["start"] + 1, "parent": 321, "children": [987]},
        987: {"start": state["start"] + 2, "parent": 654, "children": []},
    }
    for row in nodes.values():
        row.update(user=0.25, system=0.125, rss=16384, status="running")

    def process(pid):
        if pid not in nodes:
            raise Gone()
        row = nodes[pid]
        if row.get("denied"):
            raise Denied()
        return SimpleNamespace(
            _proc=SimpleNamespace(create_time=lambda *, monotonic: row["start"]),
            ppid=lambda: row["parent"],
            children=lambda *, recursive: [SimpleNamespace(pid=p) for p in row["children"]],
            cpu_times=lambda: SimpleNamespace(user=row["user"], system=row["system"]),
            memory_info=lambda: SimpleNamespace(rss=row["rss"]),
            status=lambda: row["status"],
        )

    psutil.Process = process
    return nodes, {"pid": 321, "start_ticks": 1_700_000_000_125000}


def test_native_resources_sum_descendants_in_bytes_and_seconds(tree, monkeypatch):
    nodes, binding = tree
    monkeypatch.setattr(fit.platform, "system", lambda: "Darwin")
    result = fit.resource_snapshot(binding)
    assert result["memory_available_bytes"] == 2**30
    assert result["process_tree_rss_bytes"] == len(nodes) * 16384
    assert result["process_tree_cpu_seconds"] == 1.125
    assert result["process_count"] == 3
    assert result["missing_reasons"] == {}
    assert "observed_live_descendants" in result["scope"]["processes"]
    assert "shared_pages" in result["scope"]["rss"]
    assert "excludes_exited_children" in result["scope"]["cpu"]
    assert "microseconds" in result["scope"]["process_start"]


def assert_tree_missing(result):
    assert result["memory_available_bytes"] == 2**30
    for field in ("process_tree_rss_bytes", "process_tree_cpu_seconds", "process_count"):
        assert result[field] is None
        assert result["missing_reasons"][field]


@pytest.mark.parametrize(
    "change",
    ["root_reused", "gone", "denied", "reparented", "cycle", "duplicate", "zombie", "dead"],
)
def test_bad_member_refuses_whole_tree_and_keeps_host_memory(tree, change):
    nodes, binding = tree
    if change == "root_reused":
        nodes[321]["start"] += 1
    elif change == "gone":
        del nodes[987]
    elif change == "denied":
        nodes[987]["denied"] = True
    elif change == "reparented":
        nodes[654]["parent"] = 111
    elif change == "cycle":
        nodes[987]["children"] = [321]
    elif change == "duplicate":
        nodes[321]["children"] = [654, 654]
    else:
        nodes[987]["status"] = change
    assert_tree_missing(macos.resource_snapshot(binding))


@pytest.mark.parametrize(
    "key,value",
    [
        ("rss", -1),
        ("rss", True),
        ("rss", 1.5),
        ("user", -1),
        ("user", True),
        ("user", float("nan")),
        ("system", float("inf")),
        ("parent", True),
        ("start", float("nan")),
    ],
)
def test_invalid_native_values_do_not_become_plausible_resource_totals(tree, key, value):
    nodes, binding = tree
    nodes[987][key] = value
    assert_tree_missing(macos.resource_snapshot(binding))


@pytest.mark.parametrize("change", ["start", "parent", "children", "counter"])
def test_member_mutation_after_first_tree_read_is_missing(tree, monkeypatch, change):
    nodes, binding = tree
    original = macos._row
    calls = 0

    def changed(pid, psutil):
        nonlocal calls
        calls += 1
        if calls == 4:
            if change == "start":
                nodes[987]["start"] += 1
            elif change == "parent":
                nodes[987]["parent"] = 321
            elif change == "children":
                nodes[654]["children"] = []
            else:
                nodes[987]["user"] = 0
        return original(pid, psutil)

    monkeypatch.setattr(macos, "_row", changed)
    assert_tree_missing(macos.resource_snapshot(binding))


def test_host_memory_failure_preserves_complete_process_tree(tree, monkeypatch):
    def unavailable():
        raise PreflightError("system_memory_unavailable")

    monkeypatch.setattr(macos, "memory_available", unavailable)
    result = macos.resource_snapshot(tree[1])
    assert result["memory_available_bytes"] is None
    assert result["missing_reasons"] == {"memory_available_bytes": "system_memory_unavailable"}
    assert result["process_count"] == 3
