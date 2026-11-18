"""The frozen model manifest and LM observer reject changes before requests."""

import copy
from pathlib import Path

import pytest

from inferyard.platforms.model_asset_manifest import model_asset_manifest
from inferyard.errors import PreflightError
from inferyard.contracts.validation import json_bytes
from inferyard.platforms.engine_fit_lmstudio import _model_row
from inferyard.platforms.identity import hash_file
from inferyard.platforms.local_engines import installation
from inferyard.platforms.local_engines.assets import assets, preflight_assets


def write_json(path, value):
    path.write_bytes(json_bytes(value))
    return hash_file(path).sha256


def setup_assets(tmp_path, monkeypatch):
    monkeypatch.setattr(installation, "darwin", lambda: None)
    engine_dir = tmp_path / "runtime"
    engine_dir.mkdir()
    executable = engine_dir / "binary"
    executable.write_bytes(b"fixture")
    declaration = {
        "schema_version": 3,
        "definition": "local-engine-installation.v1",
        "family": "llama-cpp",
        "executable": str(executable),
        "working_directory": str(tmp_path),
        "runtime_roots": [str(engine_dir)],
        "environment": {},
    }
    snapshot = installation.freeze_installation(declaration)
    runtime_path = tmp_path / "runtime.json"
    runtime_hash = write_json(runtime_path, snapshot)
    model = tmp_path / "model"
    model.mkdir()
    (model / "config.json").write_text("{}")
    (model / "weights.safetensors").write_bytes(b"weights")
    template = model / "chat_template.jinja"
    template.write_text("{{ messages }}")
    manifest = model_asset_manifest(model)
    manifest_path = tmp_path / "assets.json"
    manifest_hash = write_json(manifest_path, manifest)
    config = {
        "engine": {
            "family": "llama-cpp",
            "installation_path": str(runtime_path),
            "installation_sha256": runtime_hash,
        },
        "model": {
            "kind": "directory",
            "local_path": str(model),
            "assets_path": str(manifest_path),
            "assets_sha256": manifest_hash,
            "content_sha256": manifest["sha256"],
            "template_path": str(template),
            "template_sha256": hash_file(template).sha256,
        },
    }
    return config, model, manifest


def test_model_cached_hash_detects_same_mtime_edit(tmp_path, monkeypatch):
    import os

    config, model, _ = setup_assets(tmp_path, monkeypatch)
    assets(config)
    weight = model / "weights.safetensors"
    before = weight.stat()
    weight.write_bytes(b"changed")
    os.utime(weight, ns=(before.st_atime_ns, before.st_mtime_ns))
    with pytest.raises(PreflightError, match="model_assets_changed"):
        assets(config)
