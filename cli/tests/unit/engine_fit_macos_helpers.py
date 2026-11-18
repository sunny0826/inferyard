"""Synthetic Darwin identity and complete-tree refusal paths; no model execution."""

import hashlib
import os
from copy import deepcopy
from types import SimpleNamespace

import pytest

from inferyard.errors import PreflightError
from inferyard.platforms import engine_fit as fit
from inferyard.platforms import engine_fit_macos as macos
from inferyard.platforms import engine_fit_macos_listener as listeners
from inferyard.platforms import macos_identity


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
        output = "".join(
            key + str(value) + "\0" for row in state["global_rows"] for key, value in row.items()
        )
        return SimpleNamespace(returncode=0, stderr="", stdout=output)

    monkeypatch.setattr(listeners.subprocess, "run", all_listeners)
    return state, process, psutil, model


def bind(native, engine="vllm"):
    return macos.bind_service(engine, native[3], 321, "http://127.0.0.1:8080")


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


def assert_tree_missing(result):
    assert result["memory_available_bytes"] == 2**30
    for field in ("process_tree_rss_bytes", "process_tree_cpu_seconds", "process_count"):
        assert result[field] is None
        assert result["missing_reasons"][field]
