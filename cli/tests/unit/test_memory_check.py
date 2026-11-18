"""User-visible grouping, conservative decisions and saved diagnostic rejection paths."""

from copy import deepcopy

import pytest

from inferyard.application.memory_check import inspect_memory
from inferyard.contracts.memory_check import validate_memory_check
from inferyard.evidence.memory_check import read_memory_check
from inferyard.errors import ContractError, EvidenceError
from inferyard.platforms.memory_macos import reading


def observed(pid, *, parent=0, bundle=None, executable="/opt/tool", uid=501, missing=None):
    return {
        "pid": pid,
        "start_identity": pid * 100 if not missing else None,
        "parent_pid": parent,
        "parent_start_identity": parent * 100 if parent and not missing else None,
        "name": "worker",
        "uid": uid,
        "executable": executable,
        "bundle": bundle,
        "rss": reading(None if missing else pid * 1024, "fixture:rss", missing),
        "missing_reason": missing,
    }


def app(name="Browser", key="first"):
    return {"name": name, "key": key, "path": f"/Applications/{name}.app"}


def snapshot(rows, *, caller=999, truncation=None, total=None):
    return {
        "rows": rows,
        "caller_pid": caller,
        "total_pids": len(rows) if total is None else total,
        "memory_total": reading(16000, "fixture:total"),
        "memory_available": reading(5000, "fixture:available"),
        "truncation_reason": truncation,
        "collection_ns": 123,
    }


def report(rows, *, protected=(), **options):
    value = snapshot(rows, **options)
    return inspect_memory(protected, collector=lambda **_: value, uid=501)


def by_pid(value, pid):
    return next(p for p in value["processes"] if p["pid"] == pid)


def test_declared_protection_reaches_descendants_and_entire_app_not_siblings():
    data = report(
        [
            observed(10, bundle=app()),
            observed(11, parent=10),
            observed(12, bundle=app()),
            observed(13),
        ],
        protected=[10, 10],
    )
    assert data["protected_requests"] == [
        {"pid": 10, "start_identity": 1000, "status": "verified", "reason": None}
    ]
    assert by_pid(data, 11)["decision"]["status"] == "protected"
    assert data["groups"][0]["decision"]["status"] == "protected"
    assert by_pid(data, 13)["decision"]["status"] == "unknown"
