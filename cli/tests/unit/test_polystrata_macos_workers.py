"""Regression tests for macOS PolyStrata worker observation and env policy."""

from __future__ import annotations

from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.errors import PreflightError
from inferyard.platforms.identity import hash_file
from inferyard.platforms.local_engines import workers


class _FakePsutilError(Exception):
    pass


class _FakeNoSuchProcess(_FakePsutilError):
    pass


class _FakeZombieProcess(_FakeNoSuchProcess):
    pass


def _make_fake_env(
    monkeypatch: pytest.MonkeyPatch,
    *,
    root_pid: int,
    children_seq: list[set[int]],
    proc_specs: dict[int, dict],
) -> SimpleNamespace:
    children_iter = iter(children_seq)
    last_children: set[int] = children_seq[-1]

    class _FakeProcess:
        def __init__(self, pid: int):
            self.pid = pid
            if pid != root_pid and pid not in proc_specs:
                raise _FakeNoSuchProcess(pid)

        def ppid(self) -> int:
            spec = proc_specs.get(self.pid)
            if spec is None:
                raise _FakeNoSuchProcess(self.pid)
            return spec["ppid"]

        def environ(self) -> dict[str, str]:
            spec = proc_specs.get(self.pid)
            if spec is None:
                raise _FakeNoSuchProcess(self.pid)
            return dict(spec["env"])

        def is_running(self) -> bool:
            return self.pid == root_pid or self.pid in proc_specs

    def _fake_children(proc: _FakeProcess) -> set[int]:
        nonlocal last_children
        if proc.pid == root_pid:
            try:
                last_children = next(children_iter)
            except StopIteration:
                pass
            return set(last_children)
        spec = proc_specs.get(proc.pid)
        if spec is None:
            raise _FakeNoSuchProcess(proc.pid)
        return set(spec.get("children", ()))

    def _fake_state(pid: int, _psutil: object):
        spec = proc_specs.get(pid)
        if spec is None:
            raise PreflightError("service_process_unavailable")
        if spec.get("raise_once"):
            exc = spec["raise_once"].pop(0)
            raise exc
        args = spec["args"]
        raw = b"\0".join(a.encode() for a in args) + b"\0"
        return (spec["start"], args, raw, spec["exe"], spec["cwd"])

    monkeypatch.setattr(workers.native, "_children", _fake_children)
    monkeypatch.setattr(workers.native, "service_process_state", _fake_state)
    monkeypatch.setattr(workers, "lsof_records", lambda _pid, _extra: [])
    monkeypatch.setattr(workers, "mapped_file", lambda _pid, _ident, _rows: True)
    monkeypatch.setattr(workers, "mapped_closure", lambda _rows, _rf, _exe, pid=None: None)
    monkeypatch.setattr(workers.time, "sleep", lambda _s: None)

    return SimpleNamespace(
        Process=_FakeProcess,
        Error=_FakePsutilError,
        NoSuchProcess=_FakeNoSuchProcess,
        ZombieProcess=_FakeZombieProcess,
    )


def test_polystrata_ignores_macos_ioreg_telemetry_and_allows_declared_env(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    strata_bin = tmp_path / "strata"
    strata_bin.write_bytes(b"fake-strata-binary")
    runtime_files = [hash_file(strata_bin)]

    fake_psutil = _make_fake_env(
        monkeypatch,
        root_pid=100,
        children_seq=[{200, 300}, {200, 300}],
        proc_specs={
            200: {
                "ppid": 100,
                "start": 1001,
                "args": [str(strata_bin), "--serve"],
                "exe": strata_bin.resolve(),
                "cwd": tmp_path,
                "env": {
                    "PYTHONDONTWRITEBYTECODE": "1",
                    "STRATA_METAL_STREAM": "0",
                    "OMP_NUM_THREADS": "4",
                },
            },
            300: {
                "ppid": 100,
                "start": 1002,
                "args": ["ioreg", "-l", "-w0", "-r", "-c", "IOAccelerator"],
                "exe": Path("/usr/sbin/ioreg"),
                "cwd": tmp_path,
                "env": {"PYTHONDONTWRITEBYTECODE": "1"},
            },
        },
    )

    observed = workers.observe_workers(
        100,
        fake_psutil,
        runtime_files,
        {"PYTHONDONTWRITEBYTECODE": "1"},
        family="polystrata",
        allowed_extras=frozenset({"STRATA_METAL_STREAM", "OMP_NUM_THREADS"}),
        expected_cfg_env={"STRATA_METAL_STREAM": "0", "OMP_NUM_THREADS": "4"},
    )
    assert [row["pid"] for row in observed] == [200]


def test_polystrata_retries_transient_telemetry_exit_race(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    strata_bin = tmp_path / "strata"
    strata_bin.write_bytes(b"fake-strata-binary")
    runtime_files = [hash_file(strata_bin)]

    # Attempt 1 sees transient PID 301 that vanishes before state read; attempt 2 is clean.
    fake_psutil = _make_fake_env(
        monkeypatch,
        root_pid=100,
        children_seq=[{200, 301}, {200}, {200}],
        proc_specs={
            200: {
                "ppid": 100,
                "start": 1001,
                "args": [str(strata_bin), "--serve"],
                "exe": strata_bin.resolve(),
                "cwd": tmp_path,
                "env": {"PYTHONDONTWRITEBYTECODE": "1"},
            },
        },
    )

    observed = workers.observe_workers(
        100,
        fake_psutil,
        runtime_files,
        {"PYTHONDONTWRITEBYTECODE": "1"},
        family="polystrata",
    )
    assert [row["pid"] for row in observed] == [200]


def test_polystrata_still_rejects_rogue_ioreg_args_and_non_polystrata_rejects_ioreg(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    strata_bin = tmp_path / "strata"
    strata_bin.write_bytes(b"fake-strata-binary")
    runtime_files = [hash_file(strata_bin)]

    # Rogue args on /usr/sbin/ioreg must not be ignored even for polystrata.
    fake_psutil = _make_fake_env(
        monkeypatch,
        root_pid=100,
        children_seq=[{300}, {300}],
        proc_specs={
            300: {
                "ppid": 100,
                "start": 1002,
                "args": ["ioreg", "-l"],
                "exe": Path("/usr/sbin/ioreg"),
                "cwd": tmp_path,
                "env": {"PYTHONDONTWRITEBYTECODE": "1"},
            },
        },
    )
    with pytest.raises(PreflightError, match="local_engine_worker_executable_outside_runtime"):
        workers.observe_workers(
            100,
            fake_psutil,
            runtime_files,
            {"PYTHONDONTWRITEBYTECODE": "1"},
            family="polystrata",
        )

    # Exact telemetry ioreg must still be rejected for non-polystrata families.
    fake_psutil_llama = _make_fake_env(
        monkeypatch,
        root_pid=100,
        children_seq=[{300}, {300}],
        proc_specs={
            300: {
                "ppid": 100,
                "start": 1002,
                "args": ["ioreg", "-l", "-w0", "-r", "-c", "IOAccelerator"],
                "exe": Path("/usr/sbin/ioreg"),
                "cwd": tmp_path,
                "env": {"PYTHONDONTWRITEBYTECODE": "1"},
            },
        },
    )
    with pytest.raises(PreflightError, match="local_engine_worker_executable_outside_runtime"):
        workers.observe_workers(
            100,
            fake_psutil_llama,
            runtime_files,
            {"PYTHONDONTWRITEBYTECODE": "1"},
            family="llama-cpp",
        )


def test_polystrata_rejects_mismatched_declared_env_on_macos(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    strata_bin = tmp_path / "strata"
    strata_bin.write_bytes(b"fake-strata-binary")
    runtime_files = [hash_file(strata_bin)]

    fake_psutil = _make_fake_env(
        monkeypatch,
        root_pid=100,
        children_seq=[{200}, {200}],
        proc_specs={
            200: {
                "ppid": 100,
                "start": 1001,
                "args": [str(strata_bin), "--serve"],
                "exe": strata_bin.resolve(),
                "cwd": tmp_path,
                "env": {"PYTHONDONTWRITEBYTECODE": "1", "STRATA_METAL_STREAM": "1"},
            },
        },
    )

    with pytest.raises(PreflightError, match="local_engine_environment_override"):
        workers.observe_workers(
            100,
            fake_psutil,
            runtime_files,
            {"PYTHONDONTWRITEBYTECODE": "1"},
            family="polystrata",
            allowed_extras=frozenset({"STRATA_METAL_STREAM"}),
            expected_cfg_env={"STRATA_METAL_STREAM": "0"},
        )
