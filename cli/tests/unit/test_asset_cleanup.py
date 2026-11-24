"""Safety boundary tests for scoped asset deletion."""

from __future__ import annotations

import os
from pathlib import Path

import pytest

from inferyard.platforms.asset_cleanup import SEALED_EVIDENCE_MARKERS, delete_asset_safely


def test_delete_asset_safely_rejects_root_and_outside_paths(tmp_path: Path) -> None:
    models_root = tmp_path / "models"
    outside_root = tmp_path / "outside"
    models_root.mkdir()
    outside_root.mkdir()
    outside_file = outside_root / "keep.gguf"
    outside_file.write_bytes(b"keep")

    with pytest.raises(ValueError, match="asset_delete_outside_allowed_roots"):
        delete_asset_safely(models_root, allowed_roots=[models_root])
    with pytest.raises(ValueError, match="asset_delete_outside_allowed_roots"):
        delete_asset_safely(outside_file, allowed_roots=[models_root])
    with pytest.raises(ValueError, match="asset_delete_outside_allowed_roots"):
        delete_asset_safely(models_root / ".." / "outside" / "keep.gguf", allowed_roots=[models_root])
    assert outside_file.exists()


def test_delete_asset_safely_refuses_symlinks_and_broken_links(tmp_path: Path) -> None:
    if not hasattr(os, "symlink"):
        pytest.skip("symlinks not supported")

    models_root = tmp_path / "models"
    models_root.mkdir()
    real_file = models_root / "real.gguf"
    real_file.write_bytes(b"real")
    link_file = models_root / "alias.gguf"
    link_file.symlink_to(real_file)

    with pytest.raises(ValueError, match="asset_delete_symlink_refused"):
        delete_asset_safely(link_file, allowed_roots=[models_root])
    assert link_file.is_symlink()
    assert real_file.exists()

    broken = models_root / "broken.gguf"
    broken.symlink_to(models_root / "does-not-exist.gguf")
    with pytest.raises(ValueError, match="asset_delete_symlink_refused"):
        delete_asset_safely(broken, allowed_roots=[models_root])
    assert broken.is_symlink()

    dirty_dir = models_root / "dirty-dir"
    dirty_dir.mkdir()
    (dirty_dir / "weights.bin").write_bytes(b"123")
    (dirty_dir / "inner-link").symlink_to(real_file)
    with pytest.raises(ValueError, match="asset_delete_symlink_refused"):
        delete_asset_safely(dirty_dir, allowed_roots=[models_root])
    assert (dirty_dir / "weights.bin").exists()

    dir_link_target = tmp_path / "external"
    dir_link_target.mkdir()
    nested_dir_link = models_root / "nested-dir-link"
    nested_dir_link.mkdir()
    (nested_dir_link / "sub").symlink_to(dir_link_target)
    with pytest.raises(ValueError, match="asset_delete_symlink_refused"):
        delete_asset_safely(nested_dir_link, allowed_roots=[models_root])
    assert dir_link_target.exists()


def test_delete_asset_safely_protects_evidence_roots_and_markers(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    models_root = workspace / "models"
    results_root = workspace / "results"
    models_root.mkdir(parents=True)
    results_root.mkdir(parents=True)

    run_dir = results_root / "run-1"
    run_dir.mkdir()
    (run_dir / "data.txt").write_bytes(b"sealed")

    with pytest.raises(ValueError, match="asset_delete_protected_evidence"):
        delete_asset_safely(
            run_dir,
            allowed_roots=[workspace],
            protected_roots=[results_root],
        )
    with pytest.raises(ValueError, match="asset_delete_protected_evidence"):
        delete_asset_safely(
            results_root,
            allowed_roots=[workspace],
            protected_roots=[results_root],
        )
    with pytest.raises(ValueError, match="asset_delete_protected_evidence"):
        delete_asset_safely(
            workspace,
            allowed_roots=[tmp_path],
            protected_roots=[results_root],
        )

    for marker in sorted(SEALED_EVIDENCE_MARKERS):
        candidate = models_root / f"with-{marker}"
        sub = candidate / "nested"
        sub.mkdir(parents=True)
        (sub / marker).write_text("{}", encoding="utf-8")
        with pytest.raises(ValueError, match="asset_delete_protected_evidence"):
            delete_asset_safely(candidate, allowed_roots=[models_root])
        assert (sub / marker).exists()

    marker_file = models_root / "manifest.json"
    marker_file.write_text("{}", encoding="utf-8")
    with pytest.raises(ValueError, match="asset_delete_protected_evidence"):
        delete_asset_safely(marker_file, allowed_roots=[models_root])
    assert marker_file.exists()
