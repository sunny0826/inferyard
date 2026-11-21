"""Unit tests for safe asset deletion and extended asset-scan root usage reporting."""

from __future__ import annotations

import os
import struct
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.application.asset_scan import execute
from inferyard.platforms.asset_cleanup import SEALED_EVIDENCE_MARKERS, delete_asset_safely
from inferyard.platforms.asset_scan import scan_assets


def _gguf_bytes(name: str = "Demo") -> bytes:
    def string(text: str) -> bytes:
        raw = text.encode("utf-8")
        return struct.pack("<Q", len(raw)) + raw

    entries = [
        ("general.name", 8, string(name)),
        ("general.architecture", 8, string("llama")),
        ("llama.context_length", 4, struct.pack("<I", 4096)),
        ("tokenizer.chat_template", 8, string("{{ messages }}")),
    ]
    payload = bytearray(b"GGUF" + struct.pack("<IQQ", 3, 1, len(entries)))
    for key, kind, encoded in entries:
        payload.extend(string(key))
        payload.extend(struct.pack("<I", kind))
        payload.extend(encoded)
    return bytes(payload)


def test_delete_asset_safely_removes_file_and_directory(tmp_path: Path) -> None:
    models_root = tmp_path / "models"
    models_root.mkdir()
    gguf = models_root / "alpha.gguf"
    gguf.write_bytes(b"1234567890")

    res_file = delete_asset_safely(
        gguf,
        allowed_roots=[tmp_path / "missing-root", models_root],
    )
    assert res_file == {"deleted": str(gguf.resolve()), "reclaimed_bytes": 10}
    assert not gguf.exists()

    model_dir = models_root / "dir-model"
    nested = model_dir / "shards"
    nested.mkdir(parents=True)
    (model_dir / "config.json").write_bytes(b"{}")
    (nested / "part.safetensors").write_bytes(b"abcdef")

    res_dir = delete_asset_safely(model_dir, allowed_roots=[models_root])
    assert res_dir == {"deleted": str(model_dir.resolve()), "reclaimed_bytes": 8}
    assert not model_dir.exists()
    assert models_root.is_dir()


def test_delete_asset_safely_rejects_missing_and_invalid_paths(tmp_path: Path) -> None:
    models_root = tmp_path / "models"
    models_root.mkdir()
    with pytest.raises(ValueError, match="asset_delete_not_found"):
        delete_asset_safely(models_root / "missing.gguf", allowed_roots=[models_root])
    with pytest.raises(ValueError, match="asset_delete_not_found"):
        delete_asset_safely("", allowed_roots=[models_root])
    with pytest.raises(ValueError, match="asset_delete_outside_allowed_roots"):
        delete_asset_safely("relative/file.gguf", allowed_roots=[models_root])


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


def test_scan_assets_reports_roots_usage_free_space_and_total_model_bytes(tmp_path: Path) -> None:
    root_a = tmp_path / "models-a"
    root_b = tmp_path / "models-b"
    missing_root = tmp_path / "missing-models"
    engine_root = tmp_path / "engines"
    root_a.mkdir()
    root_b.mkdir()
    engine_root.mkdir()

    gguf_a = root_a / "a.gguf"
    gguf_a.write_bytes(_gguf_bytes("Alpha"))
    corrupt = root_a / "bad.gguf"
    corrupt.write_bytes(b"not-gguf")

    dir_b = root_b / "b-dir"
    dir_b.mkdir()
    (dir_b / "config.json").write_text('{"model_type":"llama"}', encoding="utf-8")
    (dir_b / "tokenizer_config.json").write_text(
        '{"chat_template":"{{ messages }}"}', encoding="utf-8"
    )
    (dir_b / "model.safetensors").write_bytes(b"weights-123456")

    eng_bin = engine_root / "llama" / "llama-server"
    eng_bin.parent.mkdir()
    eng_bin.write_bytes(b"#!/bin/sh\nexit 0\n")
    eng_bin.chmod(0o755)

    res = scan_assets(
        [root_a, root_a, "", root_b, missing_root],
        engine_roots=[engine_root, engine_root],
    )
    dir_b_size = sum(p.stat().st_size for p in dir_b.iterdir() if p.is_file())
    expected_total = gguf_a.stat().st_size + dir_b_size
    assert res["total_model_bytes"] == expected_total
    assert res["model_requests_sent"] == 0

    roots = {(item["kind"], item["path"]): item for item in res["roots"]}
    assert len(res["roots"]) == 4

    item_a = roots[("models", str(root_a.resolve()))]
    assert item_a["disk_usage_bytes"] == gguf_a.stat().st_size
    assert isinstance(item_a["disk_free_bytes"], int) and item_a["disk_free_bytes"] >= 0

    item_b = roots[("models", str(root_b.resolve()))]
    assert item_b["disk_usage_bytes"] == dir_b_size
    assert isinstance(item_b["disk_free_bytes"], int) and item_b["disk_free_bytes"] >= 0

    item_missing = roots[("models", str(missing_root.resolve()))]
    assert item_missing["disk_usage_bytes"] == 0
    assert item_missing["disk_free_bytes"] is None

    item_eng = roots[("engines", str(engine_root.resolve()))]
    assert item_eng["disk_usage_bytes"] == eng_bin.stat().st_size
    assert isinstance(item_eng["disk_free_bytes"], int) and item_eng["disk_free_bytes"] >= 0

    req = SimpleNamespace(
        command="assets scan",
        models_roots=[root_a, root_b],
        engine_roots=[engine_root],
    )
    code, cmd_result = execute(req)
    assert code == 0
    assert cmd_result.details["total_model_bytes"] == expected_total
    assert len(cmd_result.details["roots"]) == 3
