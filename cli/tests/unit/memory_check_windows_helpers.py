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
from inferyard.errors import ContractError
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
