"""Explicit source records bind to the current candidate's actual model bytes."""

import hashlib
import json
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.config import community_candidates, preparation_io
from inferyard.config.loader import load_config
from inferyard.config.preparation_io import PreparationError
from inferyard.platforms import windows_runtime_prepare
from tests.unit.test_community_candidates import candidate_request
from tests.unit.test_community_runtime import synthetic_runtime as runtime_fixture
from tests.unit.test_device_preflight import gguf

synthetic_runtime = runtime_fixture


def record_for(model, path):
    from tests.unit.test_model_source import record_for as build

    path.write_text(json.dumps(build(model)))
    return path


def macos_request(tmp_path, monkeypatch):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Darwin", "arm64"))
    monkeypatch.setattr(
        "inferyard.platforms.identity.environment_snapshot",
        lambda: {"platform": "Darwin", "profile": "balanced"},
    )
    monkeypatch.setattr(community_candidates, "fresh_macos", lambda selected, _: selected)
    monkeypatch.setattr(community_candidates, "query_version", lambda *a, **k: {})
    model = gguf(tmp_path / "chosen.gguf", "chosen")
    engine = tmp_path / "llama-server"
    engine.write_bytes(b"synthetic, never executed")
    (tmp_path / "libggml-metal.dylib").write_bytes(b"library")
    return candidate_request(tmp_path, model, engine=engine, system="Darwin"), model


def assert_remote_candidate(request):
    details = community_candidates.create(request)
    config = load_config(details["candidate"]).config.to_dict()
    assert config["model"]["repo"] == "ggml-org/models-moved"
    assert config["model"]["revision"] == "499bc8821c6b12b4e53c5bffcb21ec206f212d81"
    assert "model_lineage" not in config and "model_lineage" not in config["model"]
    assert details["ready_to_run"] is False


def test_macos_record_explicitly_binds_model_identity(tmp_path, monkeypatch):
    request, model = macos_request(tmp_path, monkeypatch)
    request = replace(
        request, model_repo=None, model_source_record=record_for(model, tmp_path / "source.json")
    )
    assert_remote_candidate(request)


def test_windows_record_binds_without_lineage(synthetic_runtime, tmp_path, monkeypatch):
    prepared = windows_runtime_prepare.prepare(synthetic_runtime[0])
    model = gguf(tmp_path / "chosen.gguf", "chosen")
    request = candidate_request(tmp_path, model, receipt=Path(prepared["receipt"]))
    monkeypatch.setattr(community_candidates, "query_version", lambda *a, **k: {})
    request = replace(
        request, model_repo=None, model_source_record=record_for(model, tmp_path / "source.json")
    )
    assert_remote_candidate(request)


@pytest.mark.parametrize("change", ["size", "hash", "bool", "parser", "repo", "revision"])
def test_conflicting_record_or_declaration_rejects_candidate(tmp_path, monkeypatch, change):
    request, model = macos_request(tmp_path, monkeypatch)
    path = record_for(model, tmp_path / "source.json")
    data = json.loads(path.read_bytes())
    if change == "size":
        data["bytes"] += 1
    elif change == "hash":
        data["sha256"] = "a" * 64
    elif change == "bool":
        data["bytes"] = True
    elif change == "parser":
        data["parser"] = "unknown"
    path.write_text(json.dumps(data))
    request = replace(
        request,
        model_source_record=path,
        model_repo="local" if change == "repo" else None,
        model_revision="other" if change == "revision" else None,
    )
    with pytest.raises(PreparationError, match="model_source_metadata_mismatch"):
        community_candidates.create(request)
    assert not request.out.exists()


def test_old_neighbor_record_is_never_searched(tmp_path, monkeypatch):
    request, model = macos_request(tmp_path, monkeypatch)
    (model.parent / "source.json").write_text("broken old record")
    config = load_config(community_candidates.create(request)["candidate"]).config.to_dict()
    assert config["model"]["repo"] == "local"
    assert config["model"]["revision"] == hashlib.sha256(model.read_bytes()).hexdigest()
