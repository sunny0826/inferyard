"""LMS model ownership requires real, stable process-tree evidence."""

import os
from copy import deepcopy
from types import SimpleNamespace

import pytest

from inferyard.errors import PreflightError
from inferyard.platforms import engine_fit_lmstudio_files as files


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
