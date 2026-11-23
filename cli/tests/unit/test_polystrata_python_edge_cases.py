"""Regression tests for PolyStrata Python path pinning, resume rebuild, and shims."""

from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import sys
from types import SimpleNamespace

from inferyard.provisioning import polystrata_prepare
from inferyard.provisioning.polystrata_preflight import GIB, run_polystrata_preflight
from inferyard.provisioning.polystrata_python import resolve_polystrata_venv_creator
from polystrata_test_helpers import (
    _HAPPY_SETUP_PY,
    _disk_usage,
    _make_fake_venv,
    _tar_gz_file,
    _write_fake_python,
    _write_fake_uv,
)


def test_polystrata_python_pins_concrete_executable_and_abs_tools_dir(tmp_path: Path, monkeypatch):
    caller_dir = tmp_path / "caller"
    out_dir = tmp_path / "out-cwd"
    caller_dir.mkdir()
    out_dir.mkdir()
    real_bin = tmp_path / "real-py"
    real_py312 = _write_fake_python(real_bin, "python3.12", 3, 12, venv_ok=True)

    shim_bin = tmp_path / "shims"
    shim_bin.mkdir()
    shim_impl = shim_bin / "_cwd_shim.py"
    shim_impl.write_text(
        "import os, sys\n"
        f"if os.path.realpath(os.getcwd()) == os.path.realpath({str(caller_dir)!r}):\n"
        f"    print('3.12\\n' + {str(real_py312)!r})\n"
        "else:\n"
        "    print('3.15')\n",
        encoding="utf-8",
    )
    if os.name == "nt":
        shim_path = shim_bin / "python.cmd"
        shim_path.write_text(f'@echo off\r\n"{sys.executable}" "{shim_impl}" %*\r\n', encoding="utf-8")
        target_sys = "Windows"
    else:
        shim_path = shim_bin / "python"
        shim_path.write_text(f'#!/bin/sh\nexec "{sys.executable}" "{shim_impl}" "$@"\n', encoding="utf-8")
        os.chmod(shim_path, 0o755)
        target_sys = "Darwin"

    monkeypatch.chdir(caller_dir)
    creator, blockers = resolve_polystrata_venv_creator(
        system=target_sys,
        venv_python=str(shim_path),
        env={"PATH": str(shim_bin)},
    )
    assert blockers == []
    assert creator is not None
    assert Path(creator.executable) == real_py312.absolute()

    captured_cmd: list[str] = []
    real_run = polystrata_prepare.subprocess.run

    def _fake_venv_run(cmd, **kwargs):
        if len(cmd) >= 3 and cmd[1:3] == ["-m", "venv"] and cmd[3] != "--help":
            captured_cmd.extend(cmd)
            _make_fake_venv(out_dir)
            return SimpleNamespace(returncode=0, stdout="", stderr="")
        return real_run(cmd, **kwargs)

    with monkeypatch.context() as mp:
        mp.setattr(polystrata_prepare.subprocess, "run", _fake_venv_run)
        py_path = polystrata_prepare._ensure_polystrata_venv(
            out_dir,
            system=target_sys,
            timeout_seconds=60,
            resolved_creator=creator,
            env={"PATH": str(shim_bin)},
        )
    assert py_path.is_file()
    assert captured_cmd == [str(real_py312.absolute()), "-m", "venv", str(out_dir / ".venv")]

    # Relative INFERYARD_TOOLS_DIR resolves to an absolute path usable after cwd=out
    rel_tools = caller_dir / "tools"
    tools_uv = _write_fake_uv(rel_tools)
    uv_creator, uv_blockers = resolve_polystrata_venv_creator(
        system=target_sys,
        env={"PATH": "", "INFERYARD_TOOLS_DIR": "tools"},
    )
    assert uv_blockers == []
    assert uv_creator is not None
    assert Path(uv_creator.executable).is_absolute()
    assert Path(uv_creator.executable) == tools_uv.absolute()


def test_polystrata_resume_rebuilds_incompatible_or_explicit_venv(tmp_path: Path, monkeypatch):
    bin_dir = tmp_path / "py-bin"
    py311 = _write_fake_python(bin_dir, "python3.11", 3, 11, venv_ok=True)
    py312 = _write_fake_python(bin_dir, "python3.12", 3, 12, venv_ok=True)

    archive = _tar_gz_file(
        tmp_path / "ps-resume.tar.gz",
        [
            ("file", "setup.py", _HAPPY_SETUP_PY.encode("utf-8")),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
        ],
    )
    archive_sha = hashlib.sha256(archive.read_bytes()).hexdigest()

    out = tmp_path / "ps-old-315"
    data = tmp_path / "data-old-315"
    (out / "serve").mkdir(parents=True)
    (out / "setup.py").write_text(_HAPPY_SETUP_PY, encoding="utf-8")
    (out / "serve" / "server.py").write_text("# server\n", encoding="utf-8")
    (out / ".inferyard-runtime.json").write_text(
        json.dumps(
            {
                "definition": "polystrata-runtime.v1",
                "source": f"local:{archive.resolve()}",
                "archive_sha256": archive_sha,
            }
        ),
        encoding="utf-8",
    )
    (data / "models").mkdir(parents=True)
    (data / "models" / "keep.gguf.part").write_bytes(b"keep")
    # Seed an old Python 3.15 .venv (default _make_fake_venv uses sys.executable == 3.15)
    _make_fake_venv(out)

    pf_old = run_polystrata_preflight(
        out,
        data,
        resume=True,
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(200 * GIB),
        env={"PATH": str(bin_dir)},
    )
    assert pf_old["reuse_existing_venv"] is False
    assert Path(pf_old["venv_creator"]["executable"]) == py312

    venv_cmds: list[list[str]] = []
    real_run = polystrata_prepare.subprocess.run

    def _intercept_rebuild(cmd, *args, **kwargs):
        if len(cmd) >= 3 and cmd[1:3] == ["-m", "venv"] and cmd[3] != "--help":
            venv_cmds.append(list(cmd))
            _make_fake_venv(out)
            return SimpleNamespace(returncode=0, stdout="", stderr="")
        return real_run(cmd, *args, **kwargs)

    with monkeypatch.context() as mp:
        mp.setattr(polystrata_prepare.subprocess, "run", _intercept_rebuild)
        res = polystrata_prepare.prepare_polystrata(
            out,
            data_dir=data,
            source=str(archive),
            resume=True,
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            env={"PATH": str(bin_dir)},
        )
    assert res["ready_to_run"] is True
    assert venv_cmds == [[str(py312), "-m", "venv", str(out / ".venv")]]
    assert (data / "models" / "keep.gguf.part").read_bytes() == b"keep"

    # Replace .venv python with one reporting 3.12 -> reused without --python, rebuilt with --python
    venv_py_bin = out / ".venv" / ("Scripts" if os.name == "nt" else "bin")
    _write_fake_python(venv_py_bin, "python", 3, 12, venv_ok=True)
    pf_ok = run_polystrata_preflight(
        out,
        data,
        resume=True,
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(200 * GIB),
        env={"PATH": str(bin_dir)},
    )
    assert pf_ok["reuse_existing_venv"] is True
    assert pf_ok["venv_creator"] is None

    pf_override = run_polystrata_preflight(
        out,
        data,
        resume=True,
        venv_python=str(py311),
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(200 * GIB),
        env={"PATH": str(bin_dir)},
    )
    assert pf_override["reuse_existing_venv"] is False
    assert Path(pf_override["venv_creator"]["executable"]) == py311


def test_polystrata_multicall_shim_realpath_and_linux_apt_minor_version(tmp_path: Path):
    bin_dir = tmp_path / "linux-multi"
    py311_novenv = _write_fake_python(bin_dir, "python3.11", 3, 11, venv_ok=False)
    _, apt_blockers = resolve_polystrata_venv_creator(
        system="Linux",
        venv_python=str(py311_novenv),
        env={"PATH": str(bin_dir)},
    )
    assert apt_blockers[0]["code"] == "polystrata_python_venv_missing"
    assert apt_blockers[0]["fix_command"] == "sudo apt install python3.11-venv"

    if os.name != "nt":
        shim_dir = tmp_path / "mise-shims"
        shim_dir.mkdir()
        target = shim_dir / "mise-multicall"
        target.write_text(
            "#!/bin/sh\n"
            'name="$(basename "$0")"\n'
            'if [ "$name" = "python3.12" ]; then\n'
            "  exit 1\n"
            "fi\n"
            'if [ "$1" = "-c" ]; then\n'
            '  case "$2" in\n'
            '    *"ensurepip"*) exit 0 ;;\n'
            '    *) echo "3.13"; exit 0 ;;\n'
            "  esac\n"
            "fi\n"
            "exit 0\n",
            encoding="utf-8",
        )
        os.chmod(target, 0o755)
        (shim_dir / "python3.12").symlink_to(target)
        py313_link = shim_dir / "python3.13"
        py313_link.symlink_to(target)

        creator, blockers = resolve_polystrata_venv_creator(
            system="Linux",
            env={"PATH": str(shim_dir)},
        )
        assert blockers == []
        assert creator is not None
        assert creator.python_version == (3, 13)
        assert Path(creator.executable) == py313_link.absolute()
