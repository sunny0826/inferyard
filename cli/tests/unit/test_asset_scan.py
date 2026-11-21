"""Unit tests for read-only local model and engine asset scanning."""

from __future__ import annotations

import argparse
import json
import os
import stat
import struct
import subprocess
import sys
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.application.asset_scan import execute
from inferyard.cli.commands import assets as assets_cmd
from inferyard.platforms.asset_scan import scan_assets


def _gguf_bytes(
    *,
    name: str = "Test Model",
    arch: str = "llama",
    context_length: int | None = 8192,
    chat_template: str | None = "{{ messages }}",
) -> bytes:
    def string(text: str) -> bytes:
        raw = text.encode("utf-8")
        return struct.pack("<Q", len(raw)) + raw

    entries: list[tuple[str, int, bytes]] = [
        ("general.name", 8, string(name)),
        ("general.architecture", 8, string(arch)),
    ]
    if context_length is not None:
        entries.append((f"{arch}.context_length", 4, struct.pack("<I", context_length)))
    if chat_template is not None:
        entries.append(("tokenizer.chat_template", 8, string(chat_template)))
    payload = bytearray(b"GGUF" + struct.pack("<IQQ", 3, 1, len(entries)))
    for key, kind, encoded in entries:
        payload.extend(string(key))
        payload.extend(struct.pack("<I", kind))
        payload.extend(encoded)
    return bytes(payload)


def _write_dir_model(
    model_dir: Path,
    *,
    weights_name: str = "model.safetensors",
    weights_data: bytes = b"weights",
    tokenizer_config: str | None = '{"chat_template": "{{ messages }}"}',
    config_text: str = '{"model_type": "qwen2", "max_position_embeddings": 32768}',
) -> None:
    model_dir.mkdir(parents=True, exist_ok=True)
    (model_dir / "config.json").write_text(config_text, encoding="utf-8")
    (model_dir / "tokenizer.json").write_text("{}", encoding="utf-8")
    (model_dir / weights_name).write_bytes(weights_data)
    if tokenizer_config is not None:
        (model_dir / "tokenizer_config.json").write_text(tokenizer_config, encoding="utf-8")


def test_scan_assets_gguf_variants(tmp_path: Path) -> None:
    good = tmp_path / "good.gguf"
    good.write_bytes(_gguf_bytes(name="Good-7B", arch="qwen2", context_length=16384))
    no_tpl = tmp_path / "no-template.gguf"
    no_tpl.write_bytes(_gguf_bytes(name="NoTpl", chat_template=None))
    corrupt = tmp_path / "corrupt.gguf"
    corrupt.write_bytes(b"not-a-gguf")

    result = scan_assets([tmp_path])
    assert result["definition"] == "asset-scan.v1"
    assert result["model_requests_sent"] == 0
    assert result["engines"] == []

    by_path = {item["path"]: item for item in result["models"]}
    assert by_path[str(good.resolve())] == {
        "path": str(good.resolve()),
        "name": "Good-7B",
        "format": "gguf",
        "size_bytes": good.stat().st_size,
        "architecture": "qwen2",
        "context_length": 16384,
        "available": True,
        "reason": None,
    }
    assert by_path[str(no_tpl.resolve())]["available"] is False
    assert by_path[str(no_tpl.resolve())]["reason"] == "chat_template_unavailable"
    assert by_path[str(corrupt.resolve())] == {
        "path": str(corrupt.resolve()),
        "name": "corrupt",
        "format": "gguf",
        "size_bytes": None,
        "architecture": None,
        "context_length": None,
        "available": False,
        "reason": "invalid_or_unreadable_gguf",
    }


def test_scan_assets_directory_models_and_depth_limit(tmp_path: Path) -> None:
    ready = tmp_path / "qwen-dir"
    _write_dir_model(ready, weights_data=b"1234567890")

    no_tpl = tmp_path / "missing-template"
    _write_dir_model(no_tpl, tokenizer_config=None)

    bad_tpl = tmp_path / "bad-template"
    _write_dir_model(bad_tpl, tokenizer_config="{invalid json")

    pt_model = tmp_path / "pytorch-dir"
    _write_dir_model(
        pt_model,
        weights_name="pytorch_model-00001-of-00002.bin",
        tokenizer_config='{"chat_template": "   "}',
    )

    incomplete = tmp_path / "incomplete"
    incomplete.mkdir()
    (incomplete / "config.json").write_text("{}", encoding="utf-8")
    (incomplete / "tokenizer.json").write_text("{}", encoding="utf-8")

    depth4 = tmp_path / "a" / "b" / "c" / "d4"
    _write_dir_model(depth4)
    depth5 = depth4 / "e5"
    _write_dir_model(depth5)

    ignored = tmp_path / ".git" / "ignored-model"
    _write_dir_model(ignored)

    result = scan_assets([tmp_path])
    by_path = {item["path"]: item for item in result["models"]}

    assert str(ready.resolve()) in by_path
    ready_entry = by_path[str(ready.resolve())]
    expected_size = sum(p.stat().st_size for p in ready.iterdir() if p.is_file())
    assert ready_entry == {
        "path": str(ready.resolve()),
        "name": "qwen-dir",
        "format": "directory",
        "size_bytes": expected_size,
        "architecture": "qwen2",
        "context_length": 32768,
        "available": True,
        "reason": None,
    }
    assert by_path[str(no_tpl.resolve())]["available"] is False
    assert by_path[str(no_tpl.resolve())]["reason"] == "chat_template_unavailable"
    assert by_path[str(bad_tpl.resolve())]["available"] is False
    assert by_path[str(bad_tpl.resolve())]["reason"] == "chat_template_unavailable"
    assert by_path[str(pt_model.resolve())]["available"] is False
    assert by_path[str(pt_model.resolve())]["reason"] == "chat_template_unavailable"

    assert str(incomplete.resolve()) not in by_path
    assert str(depth4.resolve()) in by_path
    assert str(depth5.resolve()) not in by_path
    assert str(ignored.resolve()) not in by_path


def test_scan_assets_hf_symlinks_and_unsafe_symlink_filtering(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    if not hasattr(os, "symlink"):
        pytest.skip("symlinks not supported")

    # 1. HF repo snapshot layout where models_root points directly at the snapshot directory.
    repo = tmp_path / "hub" / "models--org--demo"
    blobs = repo / "blobs"
    snap = repo / "snapshots" / "rev1"
    blobs.mkdir(parents=True)
    snap.mkdir(parents=True)

    (blobs / "cfg").write_text('{"model_type": "llama"}', encoding="utf-8")
    (blobs / "tok").write_text('{"chat_template": "{{ messages }}"}', encoding="utf-8")
    (blobs / "w").write_bytes(b"0123456789")

    (snap / "config.json").symlink_to("../../blobs/cfg")
    (snap / "tokenizer_config.json").symlink_to("../../blobs/tok")
    (snap / "model.safetensors").symlink_to("../../blobs/w")
    (snap / "broken.safetensors").symlink_to("../../blobs/missing")
    (snap / "loop.safetensors").symlink_to("loop.safetensors")
    outside_dir = tmp_path / "outside"
    outside_dir.mkdir()
    (snap / "dir_link").symlink_to(outside_dir)
    if hasattr(os, "mkfifo"):
        fifo = tmp_path / "fifo"
        os.mkfifo(fifo)
        (snap / "fifo.safetensors").symlink_to(fifo)

    res = scan_assets([snap])
    assert len(res["models"]) == 1
    entry = res["models"][0]
    assert entry["path"] == str(snap.resolve())
    assert entry["format"] == "directory"
    assert entry["available"] is True
    assert entry["size_bytes"] == (
        (blobs / "cfg").stat().st_size
        + (blobs / "tok").stat().st_size
        + (blobs / "w").stat().st_size
    )

    # 2. Outside symlink target outside models_root and outside HF hub cache must be rejected.
    (outside_dir / "secret.safetensors").write_bytes(b"secret")
    unsafe_dir = tmp_path / "models" / "unsafe"
    unsafe_dir.mkdir(parents=True)
    (unsafe_dir / "config.json").write_text("{}", encoding="utf-8")
    (unsafe_dir / "tokenizer.json").write_text("{}", encoding="utf-8")
    (unsafe_dir / "model.safetensors").symlink_to(outside_dir / "secret.safetensors")
    assert scan_assets([tmp_path / "models"])["models"] == []

    # 3. Symlink into configured HF_HUB_CACHE is allowed.
    monkeypatch.setenv("HF_HUB_CACHE", str(tmp_path / "hub"))
    linked = tmp_path / "models" / "linked-hf"
    linked.mkdir(parents=True)
    (linked / "config.json").symlink_to(blobs / "cfg")
    (linked / "tokenizer_config.json").symlink_to(blobs / "tok")
    (linked / "model.safetensors").symlink_to(blobs / "w")
    linked_res = scan_assets([tmp_path / "models"])
    assert len(linked_res["models"]) == 1
    assert linked_res["models"][0]["path"] == str(linked.resolve())
    assert linked_res["models"][0]["available"] is True


def test_scan_assets_engines_receipt_and_depth(tmp_path: Path) -> None:
    eng1_dir = tmp_path / "e1" / "bin"
    eng1_dir.mkdir(parents=True)
    receipt1 = tmp_path / "e1" / "runtime-receipt.json"
    receipt1.write_text("{}", encoding="utf-8")
    bin1 = eng1_dir / "llama-server"
    bin1.write_bytes(b"#!/bin/sh\n")
    bin1.chmod(0o755)

    eng2_dir = tmp_path / "e2"
    eng2_dir.mkdir(parents=True)
    receipt2 = eng2_dir / "runtime-receipt.json"
    receipt2.write_text("{}", encoding="utf-8")
    bin2 = eng2_dir / "llama-server.exe"
    bin2.write_bytes(b"mz")
    bin2.chmod(0o755)
    os.utime(bin1, ns=(1_000_000_000, 1_000_000_000))
    os.utime(bin2, ns=(2_000_000_000, 2_000_000_000))

    non_exec = tmp_path / "nonexec" / "llama-server"
    non_exec.parent.mkdir()
    non_exec.write_bytes(b"nope")
    non_exec.chmod(stat.S_IRUSR | stat.S_IWUSR)

    too_deep = tmp_path / "1" / "2" / "3" / "4" / "5" / "llama-server"
    too_deep.parent.mkdir(parents=True)
    too_deep.write_bytes(b"deep")
    too_deep.chmod(0o755)

    res = scan_assets([], engine_roots=[tmp_path, tmp_path / "missing-root"])
    assert len(res["engines"]) == 2
    assert res["engines"][0] == {
        "path": str(bin2.resolve()),
        "receipt": str(receipt2.resolve()),
        "modified_ns": 2_000_000_000,
    }
    assert res["engines"][1] == {
        "path": str(bin1.resolve()),
        "receipt": str(receipt1.resolve()),
        "modified_ns": 1_000_000_000,
    }


def test_execute_returns_scanned_and_sanitizes_oserror(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    _write_dir_model(tmp_path / "m1")
    req = SimpleNamespace(command="assets scan", models_roots=[tmp_path], engine_roots=None)
    code, result = execute(req)
    assert code == 0
    assert result.command == "assets scan"
    assert result.status == "scanned"
    assert result.completeness == "complete"
    assert result.details["definition"] == "asset-scan.v1"
    assert len(result.details["models"]) == 1

    def boom(*_args, **_kwargs):
        raise PermissionError("/secret/private/path")

    monkeypatch.setattr("inferyard.platforms.asset_scan.scan_assets", boom)
    err_code, err_result = execute(req)
    captured = capsys.readouterr()
    assert err_code == 4
    assert err_result.status == "error"
    assert err_result.limitations == ("io_error",)
    assert "/secret/private/path" not in captured.err
    assert "/secret/private/path" not in json.dumps(err_result.limitations)


def test_assets_command_registration_and_cold_import(tmp_path: Path) -> None:
    parser = argparse.ArgumentParser(prog="inferyard")
    subparsers = parser.add_subparsers(dest="command")
    assets_cmd.add_commands(subparsers)
    parsed = parser.parse_args(
        [
            "assets",
            "scan",
            "--models-root",
            str(tmp_path / "m1"),
            "--models-root",
            str(tmp_path / "m2"),
            "--engine-root",
            str(tmp_path / "e1"),
        ]
    )
    assert parsed.command == "assets scan"
    assert parsed.models_roots == [tmp_path / "m1", tmp_path / "m2"]
    assert parsed.engine_roots == [tmp_path / "e1"]

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
import inferyard.cli.commands.assets
import inferyard.application.asset_scan
import inferyard.platforms.asset_scan
"""
    proc = subprocess.run(
        [sys.executable, "-c", script, str(cli_root)],
        capture_output=True,
        text=True,
        check=False,
    )
    assert proc.returncode == 0, (proc.stdout, proc.stderr)
