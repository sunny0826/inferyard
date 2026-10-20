"""The frozen model manifest and LM observer reject changes before requests."""

import copy
from pathlib import Path

import pytest

from inferyard.config.engine_fit_assets import model_asset_manifest
from inferyard.evidence.storage import json_bytes
from inferyard.platforms.engine_fit_lmstudio import _model_row
from inferyard.platforms.identity import PreflightError, hash_file
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


def test_existing_v2_bytes_retained(tmp_path, monkeypatch):
    config, _, expected = setup_assets(tmp_path, monkeypatch)
    _, _, files = assets(config)
    assert len(files) == 6
    assert expected["definition"] == "model-assets.v2"


def test_sealed_identity_retains_model_manifest(tmp_path, monkeypatch):
    config, _, expected = setup_assets(tmp_path, monkeypatch)
    path = tmp_path / "binding.json"
    config["endpoint"] = {"binding_path": str(path), "binding_sha256": write_json(path, {})}
    monkeypatch.setattr(
        "inferyard.platforms.local_engines.service.validate_binding", lambda *_: None
    )
    result, _ = preflight_assets(config)
    assert result["local_engine"]["model_assets"] == expected


def test_directory_model_injection_rejected(tmp_path, monkeypatch):
    config, model, _ = setup_assets(tmp_path, monkeypatch)
    (model / "new.safetensors").write_bytes(b"injected")
    with pytest.raises(PreflightError, match="model_assets_changed"):
        assets(config)


def test_model_template_edit_rejected(tmp_path, monkeypatch):
    config, model, _ = setup_assets(tmp_path, monkeypatch)
    (model / "chat_template.jinja").write_text("wrong")
    with pytest.raises(PreflightError):
        assets(config)


def test_lm_instance_drift_and_ambiguity(tmp_path):
    model = tmp_path / "model.gguf"
    model.write_bytes(b"fixture")
    observer = {"models_root": str(tmp_path), "instance_id": "frozen"}
    row = {
        "type": "llm",
        "format": "gguf",
        "identifier": "frozen",
        "deviceIdentifier": None,
        "path": "model.gguf",
        "status": "idle",
        "queued": 0,
    }
    assert _model_row([row], observer, model) == {"status": "idle", "queued": 0}
    changed = copy.deepcopy(row)
    changed["identifier"] = "replacement"
    with pytest.raises(PreflightError, match="local_instance_unverified"):
        _model_row([changed], observer, model)
    with pytest.raises(PreflightError, match="loaded_instance_ambiguous"):
        _model_row([row, row], observer, model)


def test_legacy_entrypoint_still_refuses_formal_script(tmp_path):
    from importlib.resources import files

    from inferyard.platforms.engine_fit_entrypoints import startup_binding

    script = Path(str(files("inferyard").joinpath("data/local_engine/mlx_server.py")))
    with pytest.raises(PreflightError, match="mlx_entrypoint_changed"):
        startup_binding(
            "mlx-lm",
            ["python", "-I", "-B", str(script), "--model", str(tmp_path)],
            Path("python"),
            tmp_path,
        )


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


def test_model_index_new_reference_rejected(tmp_path, monkeypatch):
    config, model, _ = setup_assets(tmp_path, monkeypatch)
    assets(config)
    (model / "weights.index.json").write_text('{"weight_map":{"tensor":"extra.raw"}}')
    (model / "extra.raw").write_bytes(b"new shard")
    with pytest.raises(PreflightError, match="model_assets_changed"):
        assets(config)
