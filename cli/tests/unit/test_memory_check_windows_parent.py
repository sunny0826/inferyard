"""Parent PID reuse must not join unrelated Windows process generations."""

import json

import psutil
import pytest

from inferyard.contracts.memory_check import read_memory_check
from inferyard.contracts.validation import ContractError
from inferyard.platforms import memory_windows as native
from tests.unit.test_memory_check_windows import boundary, observed, report  # noqa: F401


@pytest.mark.parametrize("parent_start", [500, 1000, 2000])
@pytest.mark.usefixtures("boundary")
def test_parent_age(monkeypatch, parent_start):
    monkeypatch.setattr(native, "process_start", lambda pid: parent_start if pid == 1 else 1000)
    row = native.observe(10, psutil)
    invalid = parent_start > 1000
    assert row["parent_start_identity"] == (None if invalid else parent_start)
    assert row["parent_identity_missing_reason"] == ("parent_identity_changed" if invalid else None)
    assert row["rss"]["value"] == 100 and row["private_commit"]["value"] == 300
    data = report(monkeypatch, [row])
    assert data["coverage"]["missing"] == int(invalid)


@pytest.mark.parametrize(
    "case",
    ["valid", "equal", "changed", "missing", "generation", "outside", "absent", "unreadable"],
)
def test_verified_roots(monkeypatch, case):
    parent = observed(20, start_identity=500)
    child = observed(parent_pid=20, parent_start_identity=500)
    if case == "equal":
        parent["start_identity"] = child["parent_start_identity"] = 1000
    elif case in ("changed", "missing"):
        child.update(
            parent_start_identity=None,
            parent_identity_missing_reason="parent_identity_"
            + ("changed" if case == "changed" else "unavailable"),
        )
    elif case == "generation":
        parent["start_identity"] = 2000
    elif case == "outside":
        parent["image"] = {"key": "other-executable"}
    elif case == "unreadable":
        parent.update(
            start_identity=None,
            missing_reason="access_denied",
            rss=native.reading(None, native.RSS_SOURCE, "access_denied"),
            private_commit=native.reading(None, native.PRIVATE_SOURCE, "access_denied"),
        )
    data = report(monkeypatch, [child] if case == "absent" else [child, parent])
    group = next(g for g in data["groups"] if 10 in g["member_pids"])
    assert group["root_pids"] == (
        [20] if case in ("valid", "equal") else [10] if case in ("outside", "absent") else [10, 20]
    )
    if case == "generation":
        assert data["processes"][0]["parent_start_identity"] is None
        assert data["coverage"]["missing"] == 1


@pytest.mark.parametrize("case", ["younger", "generation", "unexplained", "root"])
def test_reader_rejects_invalid_parent(monkeypatch, tmp_path, case):
    data = report(monkeypatch, [observed(), observed(20, start_identity=500)])
    child = data["processes"][0]
    child["parent_pid"] = 20
    if case == "younger":
        child["parent_start_identity"] = 2000
        data["processes"][1]["start_identity"] = 2000
    elif case == "generation":
        child["parent_start_identity"] = 400
    elif case == "root":
        child["parent_identity_missing_reason"] = "parent_identity_changed"
        data["coverage"].update(missing=1, complete=False)
    data["groups"][0]["root_pids"] = [20]
    path = tmp_path / "invalid.json"
    path.write_text(json.dumps(data), encoding="utf-8")
    field = "groups.root_pids" if case == "root" else "processes.parent_"
    with pytest.raises(ContractError, match=field):
        read_memory_check(path)
