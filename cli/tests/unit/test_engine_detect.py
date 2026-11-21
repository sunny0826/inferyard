"""Unit tests for local engine detection, installation init, and hash caching."""

from __future__ import annotations

import argparse
import json
import os
import platform
import subprocess
import sys
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.application.engine_detect import execute
from inferyard.cli.commands.engine_detect import add_detect_subcommands
from inferyard.errors import PreflightError
from inferyard.platforms.local_engines import installation
from inferyard.platforms.local_engines.detect import (
    build_installation,
    detect_local_engines,
    write_installation,
)


def _create_fake_venv(
    venv_dir: Path,
    packages: tuple[str, ...],
    *,
    system_site_packages: str = "false",
    poison_import: bool = False,
) -> Path:
    (venv_dir / "bin").mkdir(parents=True, exist_ok=True)
    py = venv_dir / "bin" / "python"
    py.write_bytes(b"#!/bin/sh\n")
    py.chmod(0o755)
    (venv_dir / "pyvenv.cfg").write_text(
        f"home = /usr/bin\ninclude-system-site-packages = {system_site_packages}\n",
        encoding="utf-8",
    )
    site = venv_dir / "lib" / "python3.15" / "site-packages"
    site.mkdir(parents=True, exist_ok=True)
    body = "raise AssertionError('untrusted package must not be imported')\n" if poison_import else ""
    for pkg in packages:
        if pkg == "mlx-lm":
            pkg_dir = site / "mlx_lm"
            pkg_dir.mkdir(parents=True, exist_ok=True)
            (pkg_dir / "__init__.py").write_text(body, encoding="utf-8")
        elif pkg == "vllm":
            api_dir = site / "vllm" / "entrypoints" / "openai"
            api_dir.mkdir(parents=True, exist_ok=True)
            (site / "vllm" / "__init__.py").write_text(body, encoding="utf-8")
            (api_dir / "api_server.py").write_text(body, encoding="utf-8")
        elif pkg == "sglang":
            pkg_dir = site / "sglang"
            pkg_dir.mkdir(parents=True, exist_ok=True)
            (pkg_dir / "__init__.py").write_text(body, encoding="utf-8")
            (pkg_dir / "launch_server.py").write_text(body, encoding="utf-8")
    return py


def test_detect_local_engines_deduplicates_and_probes_without_importing(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    home = tmp_path / "home"
    home.mkdir()
    monkeypatch.setenv("HOME", str(home))
    monkeypatch.delenv("VIRTUAL_ENV", raising=False)
    monkeypatch.delenv("CONDA_PREFIX", raising=False)
    monkeypatch.chdir(tmp_path)

    cellar_bin = tmp_path / "cellar" / "llama.cpp" / "bin"
    cellar_lib = tmp_path / "cellar" / "llama.cpp" / "lib"
    cellar_bin.mkdir(parents=True)
    cellar_lib.mkdir(parents=True)
    real_llama = cellar_bin / "llama-server"
    real_llama.write_bytes(b"llama-bin")
    real_llama.chmod(0o755)
    link_dir = tmp_path / "brew" / "bin"
    link_dir.mkdir(parents=True)
    linked_llama = link_dir / "llama-server"
    linked_llama.symlink_to(real_llama)

    lms_bin = home / ".lmstudio" / "bin" / "lms"
    lms_bin.parent.mkdir(parents=True)
    lms_bin.write_bytes(b"lms-bin")
    lms_bin.chmod(0o755)
    lms_models = home / ".lmstudio" / "models"
    lms_models.mkdir(parents=True)

    uv_mlx = home / ".local" / "share" / "uv" / "tools" / "mlx-lm"
    _create_fake_venv(uv_mlx, ("mlx-lm",), poison_import=True)

    bad_venv = home / ".local" / "share" / "uv" / "tools" / "broken-vllm"
    _create_fake_venv(bad_venv, ("vllm",), system_site_packages="true")

    extra_sglang = tmp_path / "custom-sglang"
    _create_fake_venv(extra_sglang, ("sglang",), poison_import=True)

    def fake_which(name: str) -> str | None:
        if name == "llama-server":
            return str(linked_llama)
        if name == "lms":
            return str(lms_bin)
        return None

    monkeypatch.setattr("inferyard.platforms.local_engines.detect.shutil.which", fake_which)
    monkeypatch.setattr("inferyard.platforms.local_engines.detect._STANDARD_LLAMA_PATHS", ())

    result = detect_local_engines(extra_paths=[linked_llama, real_llama, extra_sglang])
    assert result["definition"] == "local-engine-detect.v1"
    assert result["model_requests_sent"] == 0

    by_family = {}
    for item in result["candidates"]:
        by_family.setdefault(item["family"], []).append(item)

    assert [c["executable"] for c in by_family["llama-cpp"]] == [str(real_llama.resolve())]
    assert by_family["llama-cpp"][0]["models_root"] is None

    assert len(by_family["lmstudio"]) == 1
    assert by_family["lmstudio"][0]["executable"] == str(lms_bin.resolve())
    assert by_family["lmstudio"][0]["models_root"] == str(lms_models.resolve())

    assert len(by_family["mlx-lm"]) == 1
    assert by_family["mlx-lm"][0]["runtime_roots"] == [str(uv_mlx.resolve())]
    assert by_family["mlx-lm"][0]["models_root"] is None

    assert "vllm" not in by_family  # system site-packages venv rejected
    assert len(by_family["sglang"]) == 1
    assert by_family["sglang"][0]["runtime_roots"] == [str(extra_sglang.resolve())]


def test_detect_lmstudio_models_root_none_when_absent(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    home = tmp_path / "home"
    lms_bin = home / ".lmstudio" / "bin" / "lms"
    lms_bin.parent.mkdir(parents=True)
    lms_bin.write_bytes(b"lms")
    lms_bin.chmod(0o755)
    monkeypatch.setenv("HOME", str(home))
    monkeypatch.chdir(tmp_path)
    monkeypatch.setattr("inferyard.platforms.local_engines.detect.shutil.which", lambda _: None)

    result = detect_local_engines()
    lmstudio = [c for c in result["candidates"] if c["family"] == "lmstudio"]
    assert len(lmstudio) == 1
    assert lmstudio[0]["models_root"] is None


def test_build_and_write_installation_defaults_and_safety(tmp_path: Path) -> None:
    llama_bin = tmp_path / "llama" / "llama-server"
    llama_bin.parent.mkdir(parents=True)
    llama_bin.write_bytes(b"binary")
    llama_decl = build_installation("llama-cpp", llama_bin)
    assert llama_decl["definition"] == "local-engine-installation.v1"
    assert llama_decl["working_directory"] == str(llama_bin.parent.resolve())
    assert llama_decl["runtime_roots"] == [str(llama_bin.parent.resolve())]
    assert llama_decl["environment"] == {}
    assert "models_root" not in llama_decl

    venv_py = _create_fake_venv(tmp_path / "mlx-venv", ("mlx-lm",))
    mlx_decl = build_installation("mlx-lm", venv_py)
    assert mlx_decl["runtime_roots"] == [str((tmp_path / "mlx-venv").resolve())]
    assert mlx_decl["environment"] == {
        "PYTHONDONTWRITEBYTECODE": "1",
        "HF_HUB_OFFLINE": "1",
        "TRANSFORMERS_OFFLINE": "1",
    }

    lms_bin = tmp_path / "lms" / "lms"
    lms_bin.parent.mkdir(parents=True)
    lms_bin.write_bytes(b"lms")
    models_dir = tmp_path / "lm-models"
    models_dir.mkdir()
    with pytest.raises(PreflightError, match="local_engine_models_root_required_for_lmstudio_only"):
        build_installation("lmstudio", lms_bin)
    with pytest.raises(PreflightError, match="local_engine_models_root_required_for_lmstudio_only"):
        build_installation("llama-cpp", llama_bin, models_root=models_dir)
    with pytest.raises(PreflightError, match="local_engine_direct_family_invalid"):
        build_installation("ollama", llama_bin)
    with pytest.raises(PreflightError, match="local_engine_absolute_path_required"):
        build_installation("llama-cpp", Path("relative/llama-server"))

    lms_decl = build_installation("lmstudio", lms_bin, models_root=models_dir)
    assert lms_decl["models_root"] == str(models_dir.resolve())

    out_file = tmp_path / "out" / "llama.json"
    written = write_installation(out_file, llama_decl)
    assert Path(written) == out_file.resolve()
    assert json.loads(out_file.read_text(encoding="utf-8"))["family"] == "llama-cpp"
    with pytest.raises(FileExistsError, match="output_exists"):
        write_installation(out_file, llama_decl)


def test_disk_hash_cache_reuses_unchanged_and_detects_drift(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr(installation, "darwin", lambda: None)
    monkeypatch.setattr(installation, "_FILE_CACHE", {})

    root = tmp_path / "runtime"
    root.mkdir()
    exe = root / "llama-server"
    exe.write_bytes(b"binary")
    lib = root / "lib.py"
    lib.write_text("x = 1\n", encoding="utf-8")

    decl = build_installation("llama-cpp", exe)
    snapshot = installation.freeze_installation(decl)

    cache_file = Path.home() / ".cache" / "inferyard" / "runtime-hash-cache.v1.json"
    assert cache_file.is_file()

    # Clear in-memory cache and ensure disk cache prevents calling hash_file again.
    monkeypatch.setattr(installation, "_FILE_CACHE", {})
    calls = []
    real_hash = installation.hash_file

    def counting_hash(path, expected=None):
        calls.append(Path(path))
        return real_hash(path, expected)

    monkeypatch.setattr(installation, "hash_file", counting_hash)
    installation.verify_installation(snapshot)
    assert calls == []

    # Same-mtime edit updates st_ctime_ns -> disk cache misses and detects change!
    before = lib.stat()
    lib.write_text("x = 2\n", encoding="utf-8")
    os.utime(lib, ns=(before.st_atime_ns, before.st_mtime_ns))
    monkeypatch.setattr(installation, "_FILE_CACHE", {})
    with pytest.raises(PreflightError, match="local_engine_runtime_changed"):
        installation.verify_installation(snapshot)
    assert calls

    # Corrupted/unwritable cache path must never break verification.
    lib.write_text("x = 1\n", encoding="utf-8")
    snapshot = installation.freeze_installation(decl)
    cache_file.write_text("{corrupt", encoding="utf-8")
    monkeypatch.setattr(installation, "_FILE_CACHE", {})
    installation.verify_installation(snapshot)


def test_inventory_controlled_directory_symlinks(tmp_path: Path) -> None:
    root = tmp_path / "runtime"
    versions = root / "Versions"
    version_a = versions / "A"
    version_a.mkdir(parents=True)
    (version_a / "module.py").write_text("ok = True\n", encoding="utf-8")
    current = versions / "Current"
    current.symlink_to("A")

    inv = installation.inventory(root, allowed_roots=[root])
    assert "Versions/A/module.py" in inv
    assert "Versions/Current/module.py" in inv

    # Escaping directory symlink must be rejected.
    outside = tmp_path / "outside"
    outside.mkdir()
    escape = root / "escape"
    escape.symlink_to(outside)
    with pytest.raises(PreflightError, match="local_engine_directory_symlink"):
        installation.inventory(root, allowed_roots=[root])
    escape.unlink()

    # Cyclic directory symlink must be rejected.
    loop = version_a / "loop"
    loop.symlink_to("..")
    with pytest.raises(PreflightError, match="local_engine_directory_symlink"):
        installation.inventory(root, allowed_roots=[root])
    loop.unlink()

    # Broken symlink must be rejected.
    broken = root / "broken"
    broken.symlink_to(root / "does-not-exist")
    with pytest.raises(PreflightError, match="local_engine_directory_symlink"):
        installation.inventory(root, allowed_roots=[root])


def test_application_execute_and_cli_registration(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    parser = argparse.ArgumentParser(prog="inferyard")
    subparsers = parser.add_subparsers(dest="command")
    engine = subparsers.add_parser("engine")
    actions = engine.add_subparsers(required=True)
    add_detect_subcommands(actions)

    detect_args = parser.parse_args(["engine", "detect", "--extra-path", str(tmp_path)])
    assert detect_args.command == "engine detect"
    assert detect_args.extra_paths == [tmp_path]

    exe = tmp_path / "bin" / "llama-server"
    exe.parent.mkdir(parents=True)
    exe.write_bytes(b"binary")
    out_json = tmp_path / "installations" / "llama.json"

    init_args = parser.parse_args(
        [
            "engine",
            "init-installation",
            "--family",
            "llama-cpp",
            "--executable",
            str(exe),
            "--out",
            str(out_json),
        ]
    )
    assert init_args.command == "engine init-installation"
    assert init_args.family == "llama-cpp"
    assert init_args.executable == exe
    assert init_args.out == out_json

    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr(
        "inferyard.platforms.local_engines.detect.shutil.which", lambda _: None
    )
    monkeypatch.setattr("inferyard.platforms.local_engines.detect._STANDARD_LLAMA_PATHS", ())
    code, detect_res = execute(SimpleNamespace(command="engine detect", extra_paths=[exe]))
    assert code == 0
    assert detect_res.status == "detected"
    assert detect_res.details["definition"] == "local-engine-detect.v1"
    assert len(detect_res.details["candidates"]) == 1

    code, init_res = execute(init_args)
    assert code == 0
    assert init_res.status == "prepared"
    assert init_res.details["definition"] == "local-engine-init.v1"
    assert init_res.details["family"] == "llama-cpp"
    assert Path(init_res.details["out"]).is_file()

    # Refuses overwrite.
    dup_code, dup_res = execute(init_args)
    assert dup_code == 2
    assert dup_res.status == "blocked"
    assert dup_res.limitations == ("output_exists",)

    # Sanitizes OSError without leaking path.
    def boom(*_args, **_kwargs):
        raise PermissionError("/secret/private/dir")

    monkeypatch.setattr("inferyard.platforms.local_engines.detect.detect_local_engines", boom)
    err_code, err_res = execute(SimpleNamespace(command="engine detect", extra_paths=None))
    captured = capsys.readouterr()
    assert err_code == 4
    assert err_res.limitations == ("io_error",)
    assert "/secret/private/dir" not in captured.err


def test_real_python_venv_init_installation_verifies_via_freeze(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    venv = tmp_path.resolve() / "venv"
    subprocess.run(
        [sys.executable, "-I", "-S", "-B", "-m", "venv", "--without-pip", str(venv)],
        check=True,
        capture_output=True,
        timeout=30,
    )
    site = venv / "lib" / f"python{sys.version_info.major}.{sys.version_info.minor}" / "site-packages"
    (site / "mlx_lm").mkdir(parents=True)
    (site / "mlx_lm" / "__init__.py").write_text("", encoding="utf-8")

    out_json = tmp_path / "mlx.json"
    code, res = execute(
        SimpleNamespace(
            command="engine init-installation",
            family="mlx-lm",
            executable=venv / "bin" / "python",
            out=out_json,
            working_directory=None,
            runtime_roots=None,
            models_root=None,
        )
    )
    assert code == 0, res
    expected_verified = platform.system() == "Darwin" and platform.machine() == "arm64"
    assert res.details["verified"] is expected_verified
    assert out_json.is_file()

    monkeypatch.setattr("platform.system", lambda: "Linux")
    linux_json = tmp_path / "mlx-linux.json"
    linux_code, linux_res = execute(
        SimpleNamespace(
            command="engine init-installation",
            family="mlx-lm",
            executable=venv / "bin" / "python",
            out=linux_json,
            working_directory=None,
            runtime_roots=None,
            models_root=None,
        )
    )
    assert linux_code == 0, linux_res
    assert linux_res.details["verified"] is False
    assert linux_json.is_file()


def test_cold_import_safety() -> None:
    cli_root = Path(__file__).resolve().parents[2]
    script = """
import importlib.abc
import sys

sys.path.insert(0, sys.argv[1])
BLOCKED = ("inferyard.runtime", "inferyard.extensions", "inferyard.adapters")

class RejectLiveImports(importlib.abc.MetaPathFinder):
    def find_spec(self, fullname, path=None, target=None):
        if any(fullname == prefix or fullname.startswith(prefix + ".") for prefix in BLOCKED):
            raise AssertionError("cold import hit " + fullname)

sys.meta_path.insert(0, RejectLiveImports())
import inferyard.cli.commands.engine_detect
import inferyard.application.engine_detect
import inferyard.platforms.local_engines.detect
"""
    proc = subprocess.run(
        [sys.executable, "-c", script, str(cli_root)],
        capture_output=True,
        text=True,
        check=False,
    )
    assert proc.returncode == 0, (proc.stdout, proc.stderr)
