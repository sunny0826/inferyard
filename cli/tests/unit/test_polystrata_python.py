"""Unit tests for PolyStrata Python/venv decoupling and creator resolution."""

from __future__ import annotations

import os
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.provisioning import polystrata_prepare
from inferyard.provisioning.polystrata_preflight import GIB, run_polystrata_preflight
from inferyard.provisioning.polystrata_python import (
    build_venv_command,
    resolve_polystrata_venv_creator,
)
from inferyard.provisioning.preparation_io import PreparationError
from polystrata_test_helpers import (
    _HAPPY_SETUP_PY,
    _disk_usage,
    _make_fake_venv,
    _tar_gz_file,
    _write_fake_python,
    _write_fake_uv,
)


def test_polystrata_python_resolver_prefers_uv_and_tools_dir(tmp_path: Path, monkeypatch):
    bin_dir = tmp_path / "bin"
    uv_bin = _write_fake_uv(bin_dir)
    _write_fake_python(bin_dir, "python3.12", 3, 12)
    _write_fake_python(bin_dir, "python3.15", 3, 15)

    creator, blockers = resolve_polystrata_venv_creator(
        system="Darwin",
        env={"PATH": str(bin_dir)},
    )
    assert blockers == []
    assert creator is not None
    assert creator.kind == "uv"
    assert Path(creator.executable) == uv_bin
    assert build_venv_command(creator, tmp_path / "out" / ".venv") == [
        str(uv_bin),
        "venv",
        "--no-project",
        "--seed",
        "--python",
        "3.12",
        str(tmp_path / "out" / ".venv"),
    ]

    # Also resolves uv from INFERYARD_TOOLS_DIR when PATH is empty
    tools_dir = tmp_path / "tools"
    tools_uv = _write_fake_uv(tools_dir)
    tools_creator, tools_blockers = resolve_polystrata_venv_creator(
        system="Windows" if os.name == "nt" else "Linux",
        env={"PATH": "", "INFERYARD_TOOLS_DIR": str(tools_dir)},
    )
    assert tools_blockers == []
    assert tools_creator is not None
    assert tools_creator.kind == "uv"
    assert Path(tools_creator.executable) == tools_uv.absolute()

    # _ensure_polystrata_venv invokes `uv venv --no-project --seed --python 3.12` and forwards proxy
    out = tmp_path / "out-uv"
    out.mkdir()
    captured: dict[str, object] = {}
    real_run = polystrata_prepare.subprocess.run

    def _fake_run(cmd, **kwargs):
        if Path(cmd[0]) == uv_bin and len(cmd) >= 2 and cmd[1] == "venv":
            captured["cmd"] = cmd
            captured["cwd"] = kwargs.get("cwd")
            captured["env"] = kwargs.get("env")
            _make_fake_venv(out)
            return SimpleNamespace(returncode=0, stdout="", stderr="")
        return real_run(cmd, **kwargs)

    with monkeypatch.context() as mp:
        mp.setattr(polystrata_prepare.subprocess, "run", _fake_run)
        py_path = polystrata_prepare._ensure_polystrata_venv(
            out,
            system="Darwin",
            timeout_seconds=60,
            resolved_creator=creator,
            proxy_origin="http://127.0.0.1:7890",
            env={"PATH": str(bin_dir)},
        )
    assert py_path.is_file()
    assert captured["cmd"] == [
        str(uv_bin),
        "venv",
        "--no-project",
        "--seed",
        "--python",
        "3.12",
        str(out / ".venv"),
    ]
    assert captured["cwd"] == str(out)
    assert captured["env"]["HTTPS_PROXY"] == "http://127.0.0.1:7890"


def test_polystrata_python_resolver_local_priority_and_ignores_315(tmp_path: Path, monkeypatch):
    bin_dir = tmp_path / "py-bin"
    _write_fake_python(bin_dir, "python3.15", 3, 15, venv_ok=True)
    _write_fake_python(bin_dir, "python3.10", 3, 10, venv_ok=True)
    _write_fake_python(bin_dir, "python3.13", 3, 13, venv_ok=True)
    py311 = _write_fake_python(bin_dir, "python3.11", 3, 11, venv_ok=True)
    py312 = _write_fake_python(bin_dir, "python3.12", 3, 12, venv_ok=True)

    creator, blockers = resolve_polystrata_venv_creator(
        system="Linux",
        env={"PATH": str(bin_dir)},
    )
    assert blockers == []
    assert creator is not None
    assert creator.kind == "python"
    assert creator.python_version == (3, 12)
    assert Path(creator.executable) == py312

    # If 3.12 lacks venv/ensurepip, falls through to 3.11
    _write_fake_python(bin_dir, "python3.12", 3, 12, venv_ok=False)
    fallback_creator, fallback_blockers = resolve_polystrata_venv_creator(
        system="Linux",
        env={"PATH": str(bin_dir)},
    )
    assert fallback_blockers == []
    assert fallback_creator is not None
    assert fallback_creator.python_version == (3, 11)
    assert Path(fallback_creator.executable) == py311

    # Verify _ensure_polystrata_venv executes local 3.12 interpreter with -m venv
    out = tmp_path / "out-local"
    out.mkdir()
    captured: dict[str, object] = {}
    real_run = polystrata_prepare.subprocess.run

    def _fake_run(cmd, **kwargs):
        if len(cmd) >= 3 and cmd[1:3] == ["-m", "venv"] and cmd[3] != "--help":
            captured["cmd"] = cmd
            _make_fake_venv(out)
            return SimpleNamespace(returncode=0, stdout="", stderr="")
        return real_run(cmd, **kwargs)

    with monkeypatch.context() as mp:
        mp.setattr(polystrata_prepare.subprocess, "run", _fake_run)
        py_path = polystrata_prepare._ensure_polystrata_venv(
            out,
            system="Linux",
            timeout_seconds=60,
            resolved_creator=creator,
            env={"PATH": str(bin_dir)},
        )
    assert py_path.is_file()
    assert captured["cmd"] == [str(py312), "-m", "venv", str(out / ".venv")]

    # When compatible Pythons exist but all lack venv/ensurepip -> polystrata_python_venv_missing
    no_venv_dir = tmp_path / "no-venv-bin"
    _write_fake_python(no_venv_dir, "python3.12", 3, 12, venv_ok=False)
    none_creator, venv_blockers = resolve_polystrata_venv_creator(
        system="Linux",
        env={"PATH": str(no_venv_dir)},
    )
    assert none_creator is None
    assert len(venv_blockers) == 1
    assert venv_blockers[0]["code"] == "polystrata_python_venv_missing"
    assert "python3.12-venv" in venv_blockers[0]["fix_command"]


def test_polystrata_python_resolver_explicit_python(tmp_path: Path):
    bin_dir = tmp_path / "explicit-bin"
    _write_fake_uv(bin_dir)
    py311 = _write_fake_python(bin_dir, "python3.11", 3, 11, venv_ok=True)
    py315 = _write_fake_python(bin_dir, "python3.15", 3, 15, venv_ok=True)
    py312_novenv = _write_fake_python(bin_dir, "python3.12", 3, 12, venv_ok=False)

    # Explicit --python overrides uv
    creator, blockers = resolve_polystrata_venv_creator(
        system="Darwin",
        venv_python=str(py311),
        env={"PATH": str(bin_dir)},
    )
    assert blockers == []
    assert creator is not None
    assert creator.kind == "python"
    assert creator.explicit is True
    assert creator.python_version == (3, 11)
    assert Path(creator.executable) == py311

    # Explicit Python 3.15 rejected
    c_315, b_315 = resolve_polystrata_venv_creator(
        system="Darwin",
        venv_python=str(py315),
        env={"PATH": str(bin_dir)},
    )
    assert c_315 is None
    assert b_315[0]["code"] == "polystrata_python_unsupported"

    # Explicit missing path rejected
    c_missing, b_missing = resolve_polystrata_venv_creator(
        system="Darwin",
        venv_python=str(tmp_path / "missing-python"),
        env={"PATH": str(bin_dir)},
    )
    assert c_missing is None
    assert b_missing[0]["code"] == "polystrata_python_unsupported"

    # Explicit 3.12 missing venv/ensurepip rejected with polystrata_python_venv_missing
    c_novenv, b_novenv = resolve_polystrata_venv_creator(
        system="Linux",
        venv_python=str(py312_novenv),
        env={"PATH": str(bin_dir)},
    )
    assert c_novenv is None
    assert b_novenv[0]["code"] == "polystrata_python_venv_missing"


def test_prepare_polystrata_rejects_python_315_only_in_stage1_preflight(tmp_path: Path):
    archive = _tar_gz_file(
        tmp_path / "ps.tar.gz",
        [
            ("file", "setup.py", _HAPPY_SETUP_PY.encode("utf-8")),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
        ],
    )
    only_315 = tmp_path / "only-315"
    _write_fake_python(only_315, "python3.15", 3, 15, venv_ok=True)
    _write_fake_python(only_315, "python3", 3, 15, venv_ok=True)

    out = tmp_path / "ps-blocked"
    data = tmp_path / "data-blocked"
    with pytest.raises(PreparationError) as exc_info:
        polystrata_prepare.prepare_polystrata(
            out,
            data_dir=data,
            source=str(archive),
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            env={"PATH": str(only_315)},
        )
    assert exc_info.value.reason == "polystrata_python_unsupported"
    assert exc_info.value.details["failure_stage"] == "preflight"
    assert exc_info.value.details["blockers"][0]["fix_command"]
    assert not out.exists()
    assert not (out / ".venv").exists()

    # _ensure_polystrata_venv must also fail closed when no compatible creator is resolved
    out_direct = tmp_path / "out-direct"
    out_direct.mkdir()
    with pytest.raises(PreparationError) as exc_direct:
        polystrata_prepare._ensure_polystrata_venv(
            out_direct,
            system="Darwin",
            timeout_seconds=30,
            env={"PATH": ""},
        )
    assert exc_direct.value.reason == "polystrata_python_unsupported"


def test_linux_preflight_and_ensure_venv_use_same_creator(tmp_path: Path, monkeypatch):
    bin_dir = tmp_path / "linux-bin"
    py3 = _write_fake_python(bin_dir, "python3", 3, 15, venv_ok=True)
    py312 = _write_fake_python(bin_dir, "python3.12", 3, 12, venv_ok=True)

    checked_paths: list[str] = []

    def _recording_venv_check(py_exe: str) -> bool:
        checked_paths.append(py_exe)
        return True

    report = run_polystrata_preflight(
        tmp_path / "out-linux",
        tmp_path / "data-linux",
        platform_info=("Linux", "x64"),
        linux_venv_check=_recording_venv_check,
        gpu_probe=lambda _sys: {
            "status": "available",
            "devices": [{"vendor": "nvidia", "driver_version": "580.65.06"}],
        },
        disk_usage_fn=_disk_usage(200 * GIB),
        env={"PATH": str(bin_dir)},
    )
    assert checked_paths == [str(py312)]
    assert str(py3) not in checked_paths
    assert report["venv_creator"]["kind"] == "python"
    assert Path(report["venv_creator"]["executable"]) == py312

    archive = _tar_gz_file(
        tmp_path / "ps-linux.tar.gz",
        [
            ("file", "setup.py", _HAPPY_SETUP_PY.encode("utf-8")),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
        ],
    )
    out_linux = tmp_path / "out-linux-run"
    venv_cmds: list[list[str]] = []
    real_run = polystrata_prepare.subprocess.run

    def _intercept_venv_run(cmd, *args, **kwargs):
        if len(cmd) >= 3 and cmd[1:3] == ["-m", "venv"]:
            venv_cmds.append(list(cmd))
            _make_fake_venv(out_linux)
            return SimpleNamespace(returncode=0, stdout="", stderr="")
        return real_run(cmd, *args, **kwargs)

    monkeypatch.setattr(polystrata_prepare.subprocess, "run", _intercept_venv_run)
    res = polystrata_prepare.prepare_polystrata(
        out_linux,
        data_dir=tmp_path / "data-linux-run",
        source=str(archive),
        platform_info=("Linux", "x64"),
        linux_venv_check=_recording_venv_check,
        gpu_probe=lambda _sys: {
            "status": "available",
            "devices": [{"vendor": "nvidia", "driver_version": "580.65.06"}],
        },
        disk_usage_fn=_disk_usage(200 * GIB),
        env={"PATH": str(bin_dir)},
    )
    assert res["ready_to_run"] is True
    assert venv_cmds == [[str(py312), "-m", "venv", str(out_linux / ".venv")]]
