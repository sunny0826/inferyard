"""Regression tests for local-engine re-verification caching and lightweight runtime freeze."""

from __future__ import annotations

import hashlib
from pathlib import Path

import pytest

from inferyard.contracts.validation import json_bytes
from inferyard.platforms.identity import FileIdentity, hash_file
from inferyard.platforms.local_engines import installation, service


def test_freeze_installation_hashes_only_executable_not_venv_files(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(installation, "darwin", lambda: None)
    venv_dir = tmp_path / "conda-env"
    (venv_dir / "conda-meta").mkdir(parents=True)
    bin_dir = venv_dir / "bin"
    bin_dir.mkdir()
    exe = bin_dir / "python"
    exe.write_bytes(b"#!/bin/sh\nexit 0\n")
    exe.chmod(0o755)
    site_dir = venv_dir / "lib" / "python3.12" / "site-packages" / "vllm"
    site_dir.mkdir(parents=True)
    for idx in range(50):
        (site_dir / f"mod_{idx}.py").write_text(f"X = {idx}\n", encoding="utf-8")

    decl = {
        "schema_version": 3,
        "definition": "local-engine-installation.v1",
        "family": "vllm",
        "executable": str(exe),
        "working_directory": str(venv_dir),
        "runtime_roots": [str(venv_dir)],
        "environment": {"HF_ENDPOINT": "https://hf-mirror.com"},
    }
    snap = installation.freeze_installation(decl)
    assert snap["python"] is None
    assert len(snap["roots"]) == 1
    assert len(snap["roots"][0]["entries"]) == 1
    assert snap["roots"][0]["entries"][0]["path"] == "python"


def test_verify_bound_service_reuses_asset_cache_when_bound_files_omitted(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    runtime_file = tmp_path / "server.py"
    runtime_file.write_text("print('ok')\n", encoding="utf-8")
    model_file = tmp_path / "model.gguf"
    model_file.write_bytes(b"gguf-bytes")

    binding_doc = {
        "schema_version": 3,
        "definition": "local-engine-binding.v1",
        "installation_sha256": "a" * 64,
        "service": {"pid": 1234, "origin": "http://127.0.0.1:48857"},
    }
    raw_binding = json_bytes(binding_doc)
    binding_path = tmp_path / "binding.json"
    binding_path.write_bytes(raw_binding)
    binding_sha = hashlib.sha256(raw_binding).hexdigest()

    config = {
        "endpoint": {
            "binding_path": str(binding_path),
            "binding_sha256": binding_sha,
        },
        "engine": {
            "family": "polystrata",
            "installation_path": str(tmp_path / "installation.json"),
            "installation_sha256": "a" * 64,
        },
        "model": {
            "local_path": str(model_file),
            "kind": "gguf",
            "assets_path": str(tmp_path / "assets.json"),
            "assets_sha256": "b" * 64,
            "content_sha256": "c" * 64,
            "template_path": str(tmp_path / "template.jinja"),
            "template_sha256": "d" * 64,
        },
    }

    monkeypatch.setattr(service, "_ASSET_CACHE", {})
    monkeypatch.setattr(service, "_require_family_platform", lambda _f: None)
    monkeypatch.setattr(service, "validate_binding", lambda doc, _cfg: doc)
    monkeypatch.setattr(service, "_bind", lambda _cfg, _rt, _rf: binding_doc)

    assets_calls = 0

    def fake_assets(_cfg) -> tuple[dict, list[FileIdentity], list[FileIdentity]]:
        nonlocal assets_calls
        assets_calls += 1
        return {"installation": {"family": "polystrata"}}, [hash_file(runtime_file)], [
            hash_file(model_file)
        ]

    monkeypatch.setattr(service, "assets", fake_assets)

    assert service.verify_bound_service(config) == binding_doc["service"]
    assert assets_calls == 1

    # Subsequent adapter._bound() calls (bound_files=None) reuse _ASSET_CACHE.
    assert service.verify_bound_service(config) == binding_doc["service"]
    assert service.verify_bound_service(config) == binding_doc["service"]
    assert assets_calls == 1

    # Mutating any tracked file invalidates stamp cache and triggers full re-inspection.
    runtime_file.write_text("print('mutated')\n", encoding="utf-8")
    assert service.verify_bound_service(config) == binding_doc["service"]
    assert assets_calls == 2
