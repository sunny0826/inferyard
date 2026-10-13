"""LMS model ownership requires real, stable process-tree evidence."""

import os
from copy import deepcopy
from types import SimpleNamespace

import pytest

from inferyard.platforms import engine_fit_lmstudio_files as files
from inferyard.platforms.identity import PreflightError


class NativeError(Exception):
    pass


@pytest.fixture
def tree(tmp_path, monkeypatch):
    model = tmp_path / "model.gguf"
    model.write_bytes(b"fixture weight bytes")
    uid = os.getuid()
    nodes = {
        321: {"start": 100, "parent": 1, "children": [654]},
        654: {"start": 200, "parent": 321, "children": [987]},
        987: {"start": 300, "parent": 654, "children": []},
    }
    for row in nodes.values():
        row.update(uids=(uid, uid, uid), status="running")
    rows = {
        321: [],
        654: [],
        987: [
            {
                "p": 987,
                "t": "REG",
                "f": "txt",
                "D": hex(model.stat().st_dev),
                "i": str(model.stat().st_ino),
            }
        ],
    }

    def process(pid):
        if pid not in nodes:
            raise NativeError("private process error")
        row = nodes[pid]
        if row.get("denied"):
            raise NativeError("private process permission")
        return SimpleNamespace(
            ppid=lambda: row["parent"],
            uids=lambda: row["uids"],
            children=lambda *, recursive: [SimpleNamespace(pid=child) for child in row["children"]],
            status=lambda: row["status"],
        )

    psutil = SimpleNamespace(
        Process=process,
        Error=NativeError,
        STATUS_ZOMBIE="zombie",
        STATUS_DEAD="dead",
    )
    monkeypatch.setattr(files, "process_start_ticks", lambda pid: nodes[pid]["start"])
    monkeypatch.setattr(files, "lsof_records", lambda pid, selectors, **kw: deepcopy(rows[pid]))
    return model, nodes, rows, psutil


def verify(tree):
    files.verify_model_file(321, 100, tree[0], psutil=tree[3])


@pytest.mark.parametrize("descriptor", ["txt", "0", "3", "100"])
def test_stable_descendant_mapping_or_numeric_fd_is_observable(tree, descriptor):
    tree[2][987][0]["f"] = descriptor
    verify(tree)


@pytest.mark.parametrize(
    "key,value",
    [
        ("p", 321),
        ("p", True),
        ("f", "cwd"),
        ("f", "mem"),
        ("f", "3r"),
        ("f", ""),
        ("t", "DIR"),
        ("i", "0"),
        ("D", "0"),
        ("D", None),
    ],
)
def test_non_mapping_or_wrong_inode_records_do_not_prove_ownership(tree, key, value):
    tree[2][987][0][key] = value
    with pytest.raises(PreflightError, match="model_file_not_observed"):
        verify(tree)


@pytest.mark.parametrize("change", ["reused", "parent", "children", "removed", "file"])
def test_owner_changes_while_reading_files_fail_closed(tree, monkeypatch, change):
    original = files.lsof_records

    def mutated(pid, selectors, **kwargs):
        result = original(pid, selectors, **kwargs)
        if pid == 987:
            if change == "reused":
                tree[1][987]["start"] += 1
            elif change == "parent":
                tree[1][987]["parent"] = 1
            elif change == "children":
                tree[1][654]["children"] = []
            elif change == "removed":
                tree[1][987]["status"] = "dead"
            else:
                tree[0].write_bytes(b"changed weight contents")
        return result

    monkeypatch.setattr(files, "lsof_records", mutated)
    with pytest.raises(PreflightError):
        verify(tree)


@pytest.mark.parametrize("change", ["other_user", "denied", "wrong_parent", "duplicate", "cycle"])
def test_unstable_or_unreadable_subtree_never_yields_partial_success(tree, change):
    if change == "other_user":
        tree[1][987]["uids"] = (os.getuid() + 1,) * 3
    elif change == "denied":
        tree[1][654]["denied"] = True
    elif change == "wrong_parent":
        tree[1][987]["parent"] = 1
    elif change == "duplicate":
        tree[1][654]["children"] = [987, 987]
    else:
        tree[1][987]["children"] = [321]
    with pytest.raises(PreflightError) as error:
        verify(tree)
    assert "private" not in str(error.value)


def test_unreadable_lsof_is_a_failure_even_with_other_valid_members(tree, monkeypatch):
    def denied(pid, selectors, **kwargs):
        raise PreflightError("listener_identity_unavailable")

    monkeypatch.setattr(files, "lsof_records", denied)
    with pytest.raises(PreflightError):
        verify(tree)


def test_additional_gguf_after_target_is_always_rejected(tree):
    tree[2][321] = [dict(tree[2][987][0], p=321)]
    tree[2][987].append(
        {
            "p": 987,
            "t": "REG",
            "f": "9",
            "D": "1",
            "i": "5",
            "n": "/private/draft.gguf",
        }
    )
    with pytest.raises(PreflightError, match="additional_gguf_observed"):
        verify(tree)


def test_service_tree_has_a_small_hard_cap(tree):
    nodes = tree[1]
    for pid in range(1000, 1006):
        nodes[pid] = dict(nodes[987], start=pid, parent=321, children=[])
        nodes[321]["children"].append(pid)
    with pytest.raises(PreflightError, match="file_owner_unverified"):
        verify(tree)


def test_all_process_queries_share_one_deadline(tree, monkeypatch):
    clock, deadlines = [100.0], []
    original = files.lsof_records
    monkeypatch.setattr(files.time, "monotonic", lambda: clock[0])

    def slow(pid, selectors, *, deadline):
        deadlines.append(deadline)
        clock[0] += 4.0
        return original(pid, selectors, deadline=deadline)

    monkeypatch.setattr(files, "lsof_records", slow)
    with pytest.raises(PreflightError, match="observer_timeout"):
        files.verify_model_file(321, 100, tree[0], psutil=tree[3], deadline=108.0)
    assert deadlines == [108.0, 108.0]


def test_deadline_expiry_never_spawns_lsof(tree, monkeypatch):
    monkeypatch.setattr(files.time, "monotonic", lambda: 100.0)

    def forbidden(*args, **kwargs):
        raise AssertionError("expired observation must not start another command")

    monkeypatch.setattr(files, "lsof_records", forbidden)
    with pytest.raises(PreflightError, match="observer_timeout"):
        files.verify_model_file(321, 100, tree[0], psutil=tree[3], deadline=99.0)
