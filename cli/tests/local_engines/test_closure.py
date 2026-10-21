"""Unavailable optional codecs are recorded; mapped unresolved libraries block."""

from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.platforms.identity import PreflightError, hash_file
from inferyard.platforms.local_engines import closure, workers


def test_optional_missing_recorded_but_mapped_blocks(tmp_path, monkeypatch):
    executable, optional = tmp_path / "python", tmp_path / "optional.dylib"
    for path in (executable, optional):
        path.write_bytes(b"fixture")
    monkeypatch.setattr(
        closure,
        "_commands",
        lambda path: (["@rpath/absent.dylib"], []) if path == optional else ([], []),
    )
    result = closure.verify_libraries([executable, optional], executable)
    assert result == [
        {"path": str(optional), "dependency": "@rpath/absent.dylib", "reason": "unresolved"}
    ]
    assert (
        closure.verify_libraries([executable, optional], executable, mapped_paths=[executable])
        == []
    )
    with pytest.raises(PreflightError, match="mapped_library_closure_unverified"):
        closure.verify_libraries(
            [executable, optional], executable, mapped_paths=[executable, optional]
        )


def test_mapped_dependency_outside_inventory_blocks(tmp_path, monkeypatch):
    executable, library = tmp_path / "python", tmp_path / "external.dylib"
    for path in (executable, library):
        path.write_bytes(b"fixture")
    monkeypatch.setattr(
        closure, "_commands", lambda path: ([str(library)], []) if path == executable else ([], [])
    )
    with pytest.raises(PreflightError, match="mapped_library_closure_unverified"):
        closure.verify_libraries([executable], executable, mapped_paths=[executable])


def test_worker_identity_and_process_reuse(tmp_path, monkeypatch):
    executable = tmp_path / "python"
    executable.write_bytes(b"fixture")
    identity = hash_file(executable)

    def process(pid):
        return SimpleNamespace(
            children=lambda recursive=False: [SimpleNamespace(pid=2)] if pid == 1 else [],
            ppid=lambda: 1,
            environ=lambda: {},
        )

    psutil = SimpleNamespace(Process=process)
    state = (123, ["python"], b"python\0", executable, tmp_path)
    monkeypatch.setattr(workers.native, "_state", lambda *_: state)
    monkeypatch.setattr(
        workers,
        "lsof_records",
        lambda *_: [
            {
                "p": 2,
                "f": "txt",
                "t": "REG",
                "n": str(executable),
                "D": hex(identity.device),
                "i": str(identity.inode),
            }
        ],
    )
    result = workers.observe_workers(1, psutil, [identity], {})
    assert result[0]["pid"] == 2 and result[0]["parent_pid"] == 1
    calls = iter([state, (124, *state[1:])])
    monkeypatch.setattr(workers.native, "_state", lambda *_: next(calls))
    with pytest.raises(PreflightError, match="worker_tree_changed"):
        workers.observe_workers(1, psutil, [identity], {})


def test_worker_environment_override_blocks(tmp_path, monkeypatch):
    def process(pid):
        return SimpleNamespace(
            children=lambda recursive=False: [SimpleNamespace(pid=2)] if pid == 1 else [],
            ppid=lambda: 1,
            environ=lambda: {"PYTHONPATH": "/tmp"},
        )

    psutil = SimpleNamespace(Process=process)
    monkeypatch.setattr(
        workers.native, "_state", lambda *_: (1, [], b"", Path("/python"), tmp_path)
    )
    with pytest.raises(PreflightError, match="environment_override"):
        workers.observe_workers(1, psutil, [], {})
