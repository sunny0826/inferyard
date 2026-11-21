"""Unit tests for directory-model acquisition, multi-shard GGUF download, and grouping."""

from __future__ import annotations

import hashlib
import io
import json
import os
import struct
import sys
from pathlib import Path

import httpx
import pytest

from inferyard.application.types import CommandRequest
from inferyard.errors import PreflightError
from inferyard.platforms.asset_scan import scan_assets
from inferyard.platforms.device_preflight import discover_models
from inferyard.platforms.model_asset_manifest import model_asset_manifest
from inferyard.provisioning.model_resolve import resolve_model_url
from inferyard.provisioning.model_source_download import acquire
from inferyard.provisioning.preparation_io import PreparationError

PINNED_SHA = "73e3e38d981303bc594367cd910ea6eb48349da8"


def _write_minimal_gguf(path: Path, metadata: list[tuple[str, int, bytes]]) -> Path:
    parts = [struct.pack("<4sIQQ", b"GGUF", 3, 1, len(metadata))]
    for key, vtype, raw in metadata:
        encoded = key.encode("utf-8")
        parts.append(struct.pack("<Q", len(encoded)) + encoded + struct.pack("<I", vtype) + raw)
    path.write_bytes(b"".join(parts) + b"synthetic tensor payload")
    return path


def _gguf_string(value: str) -> bytes:
    encoded = value.encode("utf-8")
    return struct.pack("<Q", len(encoded)) + encoded


class _FailingAfterPrefixStream(httpx.SyncByteStream):
    def __init__(self, prefix: bytes) -> None:
        self._prefix = prefix

    def __iter__(self):
        yield self._prefix
        raise httpx.ReadError("simulated mid-stream disconnect")


def test_acquire_directory_model_huggingface_and_resume(tmp_path, monkeypatch):
    monkeypatch.setenv("HF_TOKEN_TEST", "secret-token")
    config_bytes = json.dumps({"model_type": "qwen3", "max_position_embeddings": 32768}).encode()
    tok_bytes = json.dumps({"chat_template": "{% for m in messages %}{{ m.content }}{% endfor %}"}).encode()
    weights_bytes = b"SAFETENSORS_WEIGHTS_CONTENT_0123456789" * 4
    files = {
        "config.json": config_bytes,
        "tokenizer_config.json": tok_bytes,
        "model.safetensors": weights_bytes,
        "README.md": b"ignored readme",
    }
    tree_payload = [
        {"type": "file", "path": name, "size": len(data)} for name, data in files.items()
    ]
    seen_auth_on_cdn: list[str | None] = []
    seen_ranges: list[str | None] = []
    fail_once = True

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal fail_once
        path = request.url.path
        if request.url.host == "cdn-lfs.example.invalid":
            seen_auth_on_cdn.append(request.headers.get("Authorization"))
            content = files["model.safetensors"]
            if fail_once:
                fail_once = False
                return httpx.Response(
                    200,
                    headers={"Content-Length": str(len(content))},
                    stream=_FailingAfterPrefixStream(content[:16]),
                )
            rng = request.headers.get("Range")
            seen_ranges.append(rng)
            if rng and rng.startswith("bytes="):
                start = int(rng.split("=", 1)[1].split("-", 1)[0])
                part = content[start:]
                return httpx.Response(
                    206,
                    headers={
                        "Content-Length": str(len(part)),
                        "Content-Range": f"bytes {start}-{len(content) - 1}/{len(content)}",
                    },
                    stream=httpx.ByteStream(part),
                )
            return httpx.Response(
                200,
                headers={"Content-Length": str(len(content))},
                stream=httpx.ByteStream(content),
            )
        if path.endswith(f"/tree/{PINNED_SHA}"):
            assert request.headers.get("Authorization") == "Bearer secret-token"
            return httpx.Response(200, json=tree_payload)
        rel = path.split(f"/resolve/{PINNED_SHA}/", 1)[1]
        if rel == "model.safetensors":
            return httpx.Response(
                302, headers={"Location": "https://cdn-lfs.example.invalid/blob/model"}
            )
        content = files[rel]
        return httpx.Response(
            200,
            headers={"Content-Length": str(len(content))},
            stream=httpx.ByteStream(content),
        )

    out_dir = tmp_path / "qwen3-mlx"
    out_dir.mkdir()
    stderr = io.StringIO()
    with httpx.Client(transport=httpx.MockTransport(handler), follow_redirects=True) as client:
        with pytest.raises(PreparationError):
            acquire(
                f"https://huggingface.co/mlx-community/Qwen3-0.6B-4bit/tree/{PINNED_SHA}",
                out_dir,
                "HF_TOKEN_TEST",
                kind="directory",
                client=client,
                resume=True,
            )
        assert (out_dir / "config.json.part").exists()
        assert (out_dir / "model.safetensors.part").stat().st_size == 16
        old_stderr = sys.stderr
        sys.stderr = stderr
        try:
            details = acquire(
                f"https://huggingface.co/mlx-community/Qwen3-0.6B-4bit/tree/{PINNED_SHA}",
                out_dir,
                "HF_TOKEN_TEST",
                kind="directory",
                client=client,
                progress_stream=True,
                resume=True,
            )
        finally:
            sys.stderr = old_stderr
    assert seen_ranges == ["bytes=16-"]

    assert details["kind"] == "directory"
    assert details["files"] == 3
    assert (out_dir / "config.json").read_bytes() == config_bytes
    assert (out_dir / "tokenizer_config.json").read_bytes() == tok_bytes
    assert (out_dir / "model.safetensors").read_bytes() == weights_bytes
    assert not (out_dir / "README.md").exists()
    source = json.loads((out_dir / "source.json").read_text())
    assert source["kind"] == "directory"
    manifest = json.loads((out_dir / "model-assets.json").read_text())
    assert manifest["definition"] == "model-assets.v2"
    assert [f["path"] for f in manifest["files"]] == [
        "config.json",
        "model.safetensors",
        "tokenizer_config.json",
    ]
    assert manifest["sha256"] == details["sha256"]
    events = [json.loads(line) for line in stderr.getvalue().strip().splitlines()]
    assert any(ev["event"] == "transfer.progress" and ev["stage"] == "downloading" for ev in events)
    assert any(ev["stage"] == "preparing" for ev in events)
    assert seen_auth_on_cdn == [None, None] or all(v is None for v in seen_auth_on_cdn)

    # Confirm scan_assets picks up the acquired directory model cleanly
    scanned = scan_assets([tmp_path])
    assert len(scanned["models"]) == 1
    model_entry = scanned["models"][0]
    assert model_entry["format"] == "directory"
    assert model_entry["available"] is True
    assert model_entry["architecture"] == "qwen3"
    assert model_entry["context_length"] == 32768


def test_acquire_directory_rejects_traversal_and_incomplete_tree(tmp_path):
    def traversal_handler(_request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json=[{"type": "file", "path": "../escape.safetensors", "size": 10}],
        )

    with httpx.Client(transport=httpx.MockTransport(traversal_handler)) as client:
        with pytest.raises(PreparationError) as exc:
            acquire(
                f"https://huggingface.co/mlx-community/bad/tree/{PINNED_SHA}",
                tmp_path / "out1",
                client=client,
            )
        assert exc.value.details["failure_cause"] == "invalid_directory_entry"

    def missing_weights_handler(_request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=[{"type": "file", "path": "config.json", "size": 2}])

    with httpx.Client(transport=httpx.MockTransport(missing_weights_handler)) as client:
        with pytest.raises(PreparationError) as exc2:
            acquire(
                f"https://huggingface.co/mlx-community/incomplete/tree/{PINNED_SHA}",
                tmp_path / "out2",
                client=client,
            )
        assert exc2.value.details["failure_cause"] == "incomplete_model_directory"


def test_resolve_directory_url_modelscope_and_hf(tmp_path):
    def handler(request: httpx.Request) -> httpx.Response:
        if request.url.host == "huggingface.co" and "/revision/main" in request.url.path:
            return httpx.Response(200, json={"sha": PINNED_SHA})
        if request.url.host == "huggingface.co" and f"/tree/{PINNED_SHA}" in request.url.path:
            return httpx.Response(
                200,
                json=[
                    {"type": "file", "path": "config.json", "size": 100},
                    {"type": "file", "path": "model.safetensors", "size": 200},
                ],
            )
        if request.url.host == "modelscope.cn":
            return httpx.Response(
                200,
                json={
                    "Success": True,
                    "Data": {
                        "Files": [
                            {"Path": "config.json", "Size": 50, "Revision": PINNED_SHA},
                            {"Path": "model.safetensors", "Size": 150, "Revision": PINNED_SHA},
                        ]
                    },
                },
            )
        return httpx.Response(404)

    with httpx.Client(transport=httpx.MockTransport(handler)) as client:
        hf = resolve_model_url(
            "https://huggingface.co/mlx-community/Qwen3-0.6B-4bit/tree/main",
            client=client,
        )
        ms = resolve_model_url(
            "https://modelscope.cn/Qwen/Qwen3-0.6B/tree/master",
            client=client,
        )

    assert hf["kind"] == "directory"
    assert hf["pinned_url"] == f"https://huggingface.co/mlx-community/Qwen3-0.6B-4bit/tree/{PINNED_SHA}"
    assert hf["bytes"] == 300
    assert ms["kind"] == "directory"
    assert ms["pinned_url"] == f"https://modelscope.cn/Qwen/Qwen3-0.6B/tree/{PINNED_SHA}"
    assert ms["bytes"] == 200


def test_acquire_sharded_gguf_all_shards_manifest_and_grouping(tmp_path):
    shards = {
        "Big-Q4_K_M-00001-of-00003.gguf": b"shard-one-data",
        "Big-Q4_K_M-00002-of-00003.gguf": b"shard-two-data",
        "Big-Q4_K_M-00003-of-00003.gguf": b"shard-three-data",
    }

    def handler(request: httpx.Request) -> httpx.Response:
        name = request.url.path.rsplit("/", 1)[-1]
        data = shards[name]
        if request.method == "HEAD":
            return httpx.Response(200, headers={"Content-Length": str(len(data))})
        return httpx.Response(
            200,
            headers={"Content-Length": str(len(data))},
            stream=httpx.ByteStream(data),
        )

    out_dir = tmp_path / "sharded"
    source_url = (
        f"https://huggingface.co/org/Big-GGUF/blob/{PINNED_SHA}/"
        "Big-Q4_K_M-00002-of-00003.gguf"
    )
    with httpx.Client(transport=httpx.MockTransport(handler)) as client:
        details = acquire(source_url, out_dir, client=client)

    assert details["kind"] == "gguf"
    assert Path(details["model"]) == out_dir / "Big-Q4_K_M-00001-of-00003.gguf"
    assert len(details["shards"]) == 3
    for name, data in shards.items():
        assert (out_dir / name).read_bytes() == data
    assert details["bytes"] == sum(len(v) for v in shards.values())
    assert hashlib.sha256(shards["Big-Q4_K_M-00001-of-00003.gguf"]).hexdigest() in json.dumps(
        details["shards"]
    )
    source = json.loads((out_dir / "source.json").read_text())
    assert source["kind"] == "gguf_sharded"
    assert len(source["shards"]) == 3


def test_multi_shard_gguf_manifest_scan_and_preflight_grouping(tmp_path):
    complete_dir = tmp_path / "complete"
    complete_dir.mkdir()
    meta_common = [
        ("general.name", 8, _gguf_string("Qwen3-32B-Q4")),
        ("general.architecture", 8, _gguf_string("llama")),
        ("llama.context_length", 4, struct.pack("<I", 8192)),
        ("tokenizer.chat_template", 8, _gguf_string("{{ eos }}")),
        ("split.count", 4, struct.pack("<I", 3)),
    ]
    shard_paths = []
    for idx in range(1, 4):
        p = _write_minimal_gguf(
            complete_dir / f"Qwen3-Q4-0000{idx}-of-00003.gguf",
            [*meta_common, ("split.no", 4, struct.pack("<I", idx - 1))],
        )
        shard_paths.append(p)

    manifest = model_asset_manifest(shard_paths[0])
    assert manifest["kind"] == "gguf"
    assert [item["path"] for item in manifest["files"]] == [p.name for p in shard_paths]

    with pytest.raises(PreflightError, match="split_gguf"):
        model_asset_manifest(shard_paths[1])

    scan = scan_assets([complete_dir])
    assert len(scan["models"]) == 1
    assert scan["models"][0]["path"] == str(shard_paths[0].resolve())
    assert scan["models"][0]["name"] == "Qwen3-32B-Q4"
    assert scan["models"][0]["size_bytes"] == sum(p.stat().st_size for p in shard_paths)
    assert scan["models"][0]["available"] is True

    preflight_models = discover_models([complete_dir])
    assert len(preflight_models) == 1
    assert preflight_models[0]["size_bytes"] == sum(p.stat().st_size for p in shard_paths)

    # Remove middle shard -> incomplete set is blocked cleanly as one logical entry
    shard_paths[1].unlink()
    with pytest.raises(PreflightError, match="split_gguf"):
        model_asset_manifest(shard_paths[0])

    incomplete_scan = scan_assets([complete_dir])
    assert len(incomplete_scan["models"]) == 1
    assert incomplete_scan["models"][0]["available"] is False
    assert incomplete_scan["models"][0]["reason"] == "split_gguf_incomplete"

    incomplete_preflight = discover_models([complete_dir])
    assert len(incomplete_preflight) == 1
    assert incomplete_preflight[0]["status"] == "unavailable"
    assert incomplete_preflight[0]["reason"] == "split_gguf_incomplete"

    # Symlinked shard rejected
    if hasattr(os, "symlink"):
        target = tmp_path / "external.gguf"
        _write_minimal_gguf(target, [*meta_common, ("split.no", 4, struct.pack("<I", 1))])
        os.symlink(target, shard_paths[1])
        with pytest.raises(PreflightError, match="symlink|split_gguf"):
            model_asset_manifest(shard_paths[0])
        assert CommandRequest("model acquire").model_kind == "auto"
