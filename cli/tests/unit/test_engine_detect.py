"""Runtime hash-cache tamper detection and symlink-escape safety tests."""

from __future__ import annotations

import os
from pathlib import Path

import pytest

from inferyard.errors import PreflightError
from inferyard.platforms.local_engines import installation
from inferyard.platforms.local_engines.detect import build_installation


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
