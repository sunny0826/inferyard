"""Regression tests for macOS PolyStrata worker observation and transient probe filtering."""

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
    monkeypatch.setattr(workers.time, "sleep", lambda _s: None)

    return SimpleNamespace(
        Process=_FakeProcess,
        Error=_FakePsutilError,
        NoSuchProcess=_FakeNoSuchProcess,
        ZombieProcess=_FakeZombieProcess,
    )


def test_polystrata_ignores_macos_ioreg_telemetry_and_allows_custom_env(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    strata_bin = tmp_path / "strata"
    strata_bin.write_bytes(b"fake-strata-binary")
    fake_ioreg = tmp_path / "ioreg"
    fake_ioreg.write_bytes(b"fake-ioreg")
    monkeypatch.setattr(workers, "_POLYSTRATA_MACOS_TELEMETRY_EXE", fake_ioreg.resolve())
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
                    "HF_ENDPOINT": "https://hf-mirror.com",
                    "STRATA_METAL_STREAM": "1",
                    "OMP_NUM_THREADS": "4",
                },
            },
            300: {
                "ppid": 100,
                "start": 1002,
                "args": ["ioreg", "-l", "-w0", "-r", "-c", "IOAccelerator"],
                "exe": fake_ioreg.resolve(),
                "cwd": tmp_path,
                "env": {},
            },
        },
    )

    observed = workers.observe_workers(
        100,
        fake_psutil,
        runtime_files,
        {},
        family="polystrata",
    )
    assert [row["pid"] for row in observed] == [200]


def test_polystrata_retries_transient_telemetry_exit_race(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    strata_bin = tmp_path / "strata"
    strata_bin.write_bytes(b"fake-strata-binary")
    runtime_files = [hash_file(strata_bin)]

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
                "env": {},
            },
        },
    )

    observed = workers.observe_workers(
        100,
        fake_psutil,
        runtime_files,
        {},
        family="polystrata",
    )
    assert [row["pid"] for row in observed] == [200]


def test_non_telemetry_children_are_recorded_in_worker_snapshot(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    strata_bin = tmp_path / "strata"
    strata_bin.write_bytes(b"fake-strata-binary")
    helper_bin = tmp_path / "helper"
    helper_bin.write_bytes(b"fake-helper-binary")
    monkeypatch.setattr(workers, "_POLYSTRATA_MACOS_TELEMETRY_EXE", helper_bin.resolve())

    fake_psutil = _make_fake_env(
        monkeypatch,
        root_pid=100,
        children_seq=[{300}, {300}],
        proc_specs={
            300: {
                "ppid": 100,
                "start": 1002,
                "args": ["ioreg", "-l"],
                "exe": helper_bin.resolve(),
                "cwd": tmp_path,
                "env": {},
            },
        },
    )
    observed = workers.observe_workers(100, fake_psutil, family="polystrata")
    assert [row["pid"] for row in observed] == [300]
