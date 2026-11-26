"""Unit tests for cross-process disk hash caching in model_snapshot."""

from __future__ import annotations

import json
import os
import struct
from pathlib import Path

import pytest

from inferyard.errors import PreflightError
from inferyard.platforms.local_engines import model_snapshot


def _write_gguf(path: Path, metadata: dict[str, str | int], payload: bytes = b"tensors") -> Path:
    parts = [struct.pack("<4sIQQ", b"GGUF", 3, 1, len(metadata))]
    for key, value in metadata.items():
        key_bytes = key.encode("utf-8")
        parts.append(struct.pack("<Q", len(key_bytes)) + key_bytes)
        if isinstance(value, str):
            val_bytes = value.encode("utf-8")
            parts.append(struct.pack("<IQ", 8, len(val_bytes)) + val_bytes)
        else:
            parts.append(struct.pack("<II", 4, value))
    parts.append(payload)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(b"".join(parts))
    return path


@pytest.fixture(autouse=True)
def _isolated_model_cache(monkeypatch, tmp_path):
    cache_file = tmp_path / "cache" / "model-hash-cache.v1.json"
    monkeypatch.setattr(model_snapshot, "_hash_cache_path", lambda: cache_file)
    model_snapshot._CACHE.clear()
    yield cache_file
    model_snapshot._CACHE.clear()


def test_split_gguf_reuses_disk_cache_across_processes(monkeypatch, tmp_path):
    shard1 = _write_gguf(
        tmp_path / "demo-00001-of-00002.gguf",
        {"general.architecture": "qwen35moe", "split.count": 2, "split.no": 0},
        payload=b"shard-one-bytes",
    )
    _write_gguf(
        tmp_path / "demo-00002-of-00002.gguf",
        {"general.architecture": "qwen35moe", "split.count": 2, "split.no": 1},
        payload=b"shard-two-bytes",
    )

    calls = 0
    real_manifest = model_snapshot.model_asset_manifest

    def counting_manifest(path, **kwargs):
        nonlocal calls
        calls += 1
        return real_manifest(path, **kwargs)

    monkeypatch.setattr(model_snapshot, "model_asset_manifest", counting_manifest)

    first_manifest, first_files = model_snapshot.snapshot(shard1)
    assert calls == 1
    assert len(first_manifest["files"]) == 2
    assert len(first_files) == 2

    # Simulate a fresh CLI subprocess by clearing only the in-memory cache.
    model_snapshot._CACHE.clear()
    second_manifest, second_files = model_snapshot.snapshot(shard1)
    assert calls == 1
    assert second_manifest == first_manifest
    assert second_files == first_files


def test_split_gguf_disk_cache_invalidates_on_modify_delete_and_symlink(monkeypatch, tmp_path):
    shard1 = _write_gguf(
        tmp_path / "demo-00001-of-00002.gguf",
        {"general.architecture": "qwen35moe", "split.count": 2, "split.no": 0},
        payload=b"shard-one",
    )
    shard2 = _write_gguf(
        tmp_path / "demo-00002-of-00002.gguf",
        {"general.architecture": "qwen35moe", "split.count": 2, "split.no": 1},
        payload=b"shard-two",
    )

    first_manifest, _ = model_snapshot.snapshot(shard1)

    # Modify shard 2 -> must re-hash and detect new digest.
    _write_gguf(
        shard2,
        {"general.architecture": "qwen35moe", "split.count": 2, "split.no": 1},
        payload=b"shard-two-updated!",
    )
    model_snapshot._CACHE.clear()
    updated_manifest, _ = model_snapshot.snapshot(shard1)
    assert updated_manifest["sha256"] != first_manifest["sha256"]

    # Replace shard 2 with a symlink -> symlinked shards now resolve cleanly.
    if os.name != "nt":
        real_copy = tmp_path / "outside.gguf"
        real_copy.write_bytes(shard2.read_bytes())
        shard2.unlink()
        shard2.symlink_to(real_copy)
        model_snapshot._CACHE.clear()
        linked_manifest, _ = model_snapshot.snapshot(shard1)
        assert linked_manifest["sha256"] == updated_manifest["sha256"]

    # Delete shard 2 -> must not use cache and must reject incomplete split GGUF.
    shard2.unlink()
    model_snapshot._CACHE.clear()
    with pytest.raises(PreflightError, match="engine_fit_split_gguf_incomplete"):
        model_snapshot.snapshot(shard1)


def test_directory_model_reuses_and_invalidates_disk_cache(monkeypatch, tmp_path):
    model_dir = tmp_path / "dir-model"
    model_dir.mkdir()
    (model_dir / "config.json").write_text('{"model_type":"qwen3"}', encoding="utf-8")
    (model_dir / "model.safetensors").write_bytes(b"weights-v1")

    calls = 0
    real_manifest = model_snapshot.model_asset_manifest

    def counting_manifest(path, **kwargs):
        nonlocal calls
        calls += 1
        return real_manifest(path, **kwargs)

    monkeypatch.setattr(model_snapshot, "model_asset_manifest", counting_manifest)

    first_manifest, first_files = model_snapshot.snapshot(model_dir)
    assert calls == 1

    model_snapshot._CACHE.clear()
    second_manifest, second_files = model_snapshot.snapshot(model_dir)
    assert calls == 1
    assert second_manifest == first_manifest
    assert second_files == first_files

    # Adding a new asset file must invalidate the cached directory snapshot.
    (model_dir / "extra.safetensors").write_bytes(b"more-weights")
    model_snapshot._CACHE.clear()
    third_manifest, _ = model_snapshot.snapshot(model_dir)
    assert calls == 2
    assert third_manifest["sha256"] != first_manifest["sha256"]


def test_tampered_or_corrupt_disk_cache_falls_back_to_rehash(
    monkeypatch, tmp_path, _isolated_model_cache
):
    model = _write_gguf(
        tmp_path / "single.gguf",
        {"general.architecture": "qwen35moe"},
        payload=b"payload",
    )
    first_manifest, _ = model_snapshot.snapshot(model)

    # Tamper cached sha256 while keeping stamps identical -> must reject and recompute.
    raw = json.loads(_isolated_model_cache.read_text(encoding="utf-8"))
    key = str(model.resolve(strict=True))
    raw["entries"][key]["manifest"]["files"][0]["sha256"] = "0" * 64
    _isolated_model_cache.write_text(json.dumps(raw), encoding="utf-8")

    model_snapshot._CACHE.clear()
    recomputed, _ = model_snapshot.snapshot(model)
    assert recomputed == first_manifest

    # Corrupt JSON -> must fall back cleanly.
    _isolated_model_cache.write_text("{not-json", encoding="utf-8")
    model_snapshot._CACHE.clear()
    recomputed_after_corrupt, _ = model_snapshot.snapshot(model)
    assert recomputed_after_corrupt == first_manifest


def test_windows_native_change_time_invalidates_disk_cache_when_mtime_preserved(
    monkeypatch, tmp_path
):
    model = _write_gguf(
        tmp_path / "win.gguf",
        {"general.architecture": "qwen35moe"},
        payload=b"payload-a",
    )
    change_time = [100]

    def fake_native_stamp(target: Path):
        st = target.stat()
        return (1, "ab" * 16, 0x20, st.st_size, 50, st.st_mtime_ns, change_time[0])

    fixed_stat = model_snapshot.file_stamp(model.stat())
    monkeypatch.setattr(model_snapshot, "file_stamp", lambda _st: fixed_stat)
    monkeypatch.setattr(model_snapshot, "_native_stamp", fake_native_stamp)

    calls = 0
    real_manifest = model_snapshot.model_asset_manifest

    def counting_manifest(path, **kwargs):
        nonlocal calls
        calls += 1
        return real_manifest(path, **kwargs)

    monkeypatch.setattr(model_snapshot, "model_asset_manifest", counting_manifest)

    first_manifest, _ = model_snapshot.snapshot(model)
    assert calls == 1

    # Same mtime & size & ChangeTime -> disk cache hit across cleared memory cache.
    model_snapshot._CACHE.clear()
    second_manifest, _ = model_snapshot.snapshot(model)
    assert calls == 1
    assert second_manifest == first_manifest

    # In-place update with identical length and preserved mtime, but incremented ChangeTime.
    original_stat = model.stat()
    _write_gguf(model, {"general.architecture": "qwen35moe"}, payload=b"payload-b")
    os.utime(model, ns=(original_stat.st_atime_ns, original_stat.st_mtime_ns))
    change_time[0] = 200

    model_snapshot._CACHE.clear()
    third_manifest, _ = model_snapshot.snapshot(model)
    assert calls == 2
    assert third_manifest["sha256"] != first_manifest["sha256"]


def test_directory_model_invalid_index_encoding_raises_preflight_error(tmp_path):
    model_dir = tmp_path / "utf16-index-model"
    model_dir.mkdir()
    (model_dir / "config.json").write_text('{"model_type":"qwen3"}', encoding="utf-8")
    (model_dir / "model.safetensors.index.json").write_bytes(
        '{"weight_map":{"a":"model.safetensors"}}'.encode("utf-16")
    )

    with pytest.raises(PreflightError, match="local_engine_model_unreadable"):
        model_snapshot.snapshot(model_dir)


def test_disk_cache_evicts_oldest_entries_to_stay_within_byte_limit(
    monkeypatch, tmp_path, _isolated_model_cache
):
    model_a = _write_gguf(
        tmp_path / "a.gguf", {"general.architecture": "qwen35moe"}, payload=b"aaa"
    )
    model_b = _write_gguf(
        tmp_path / "b.gguf", {"general.architecture": "qwen35moe"}, payload=b"bbb"
    )

    model_snapshot.snapshot(model_a)
    one_entry_bytes = _isolated_model_cache.stat().st_size

    # Cap just above one entry so adding model_b must evict model_a instead of exceeding limit.
    monkeypatch.setattr(model_snapshot, "_MAX_DISK_CACHE_BYTES", one_entry_bytes + 64)
    model_snapshot.snapshot(model_b)

    assert _isolated_model_cache.stat().st_size <= one_entry_bytes + 64
    saved = json.loads(_isolated_model_cache.read_text(encoding="utf-8"))
    assert str(model_b.resolve(strict=True)) in saved["entries"]
    assert str(model_a.resolve(strict=True)) not in saved["entries"]

    # If a single entry exceeds the byte limit, keep the existing cache file intact.
    before_raw = _isolated_model_cache.read_bytes()
    monkeypatch.setattr(model_snapshot, "_MAX_DISK_CACHE_BYTES", 32)
    model_snapshot._CACHE.clear()
    model_snapshot.snapshot(model_a)
    assert _isolated_model_cache.read_bytes() == before_raw


def test_large_weights_use_sampled_digest_and_directory_symlinks_work(tmp_path):
    blob_dir = tmp_path / "blobs"
    blob_dir.mkdir()
    big_weight = blob_dir / "big.safetensors"
    big_weight.write_bytes(b"A" * (5 * 1024 * 1024))

    model_dir = tmp_path / "hf-snapshot"
    model_dir.mkdir()
    (model_dir / "config.json").write_text('{"model_type":"qwen3"}', encoding="utf-8")
    if os.name != "nt":
        sub_real = tmp_path / "real-sub"
        sub_real.mkdir()
        (sub_real / "model.safetensors").symlink_to(big_weight)
        (model_dir / "weights").symlink_to(sub_real)
    else:
        (model_dir / "model.safetensors").write_bytes(b"A" * (5 * 1024 * 1024))

    manifest, files = model_snapshot.snapshot(model_dir)
    assert manifest["kind"] == "directory"
    assert any(item["size"] == 5 * 1024 * 1024 for item in manifest["files"])
    assert len(files) == 2
