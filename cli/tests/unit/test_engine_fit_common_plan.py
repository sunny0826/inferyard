"""Frozen single-file assets and engine capabilities never imply model equivalence."""

import struct
from copy import deepcopy

import pytest

from inferyard.application.types import CommandRequest
from inferyard.cli import main
from inferyard.config.engine_fit import digest, prepare, validate_plan
from inferyard.config.engine_fit_assets import model_asset_manifest
from inferyard.contracts.validation import ContractError
from inferyard.platforms.identity import PreflightError


def gguf(path, metadata=()):
    content = b"GGUF" + struct.pack("<IQQ", 3, 1, len(metadata))
    for name, kind, value in metadata:
        raw = name.encode()
        content += struct.pack("<Q", len(raw)) + raw + struct.pack("<I", kind)
        content += value
    path.write_bytes(content + b"synthetic bytes; not a loadable model")
    return path


@pytest.fixture
def model(tmp_path, monkeypatch):
    monkeypatch.setattr(
        "inferyard.platforms.engine_fit.host_identity",
        lambda: {"sha256": "a" * 64, "platform": "Darwin"},
    )
    return gguf(tmp_path / "model.gguf")


def test_single_gguf_plan_freezes_file_and_selects_compatible_defaults(tmp_path, model):
    plan = prepare(CommandRequest("engine-fit plan", model_path=model, out=tmp_path / "plan"))
    assert plan["definition"] == "engine_fit_plan.v2"
    assert plan["engines"] == ["llama-cpp", "lmstudio"]
    assert plan["model"]["kind"] == "gguf"
    assert plan["model"]["files"][0]["path"] == "model.gguf"
    assert len(plan["model"]["files"]) == 1
    assert validate_plan(plan) == plan
    before = plan["model"]
    model.write_bytes(model.read_bytes() + b"changed weights")
    assert model_asset_manifest(model) != before


@pytest.mark.parametrize(
    "metadata",
    [
        [("split.count", 4, struct.pack("<I", 2))],
        [("split.no", 4, struct.pack("<I", 1))],
        [("split.count", 7, b"\x01")],
        [("split.count", 6, struct.pack("<f", 1.0))],
        [("split.count", 9, struct.pack("<IQ", 4, 0))],
    ],
)
def test_split_or_invalid_split_metadata_cannot_be_frozen(tmp_path, metadata):
    model = gguf(tmp_path / "part.gguf", metadata)
    with pytest.raises(PreflightError, match="split_gguf|model_unreadable"):
        model_asset_manifest(model)


def test_single_split_metadata_is_supported(tmp_path):
    model = gguf(tmp_path / "single.gguf", [("split.count", 4, struct.pack("<I", 1))])
    assert model_asset_manifest(model)["kind"] == "gguf"


def test_file_symlink_and_non_gguf_rejected(tmp_path, model):
    link = tmp_path / "link.gguf"
    link.symlink_to(model)
    with pytest.raises(PreflightError, match="symlink"):
        model_asset_manifest(link)
    invalid = tmp_path / "not.gguf"
    invalid.write_bytes(b"not a gguf")
    with pytest.raises(PreflightError, match="model_unreadable"):
        model_asset_manifest(invalid)


@pytest.mark.parametrize("engine", ["vllm", "sglang", "mlx-lm"])
def test_directory_engines_cannot_use_frozen_gguf(tmp_path, model, engine):
    with pytest.raises(ContractError, match="engine_asset_kind"):
        prepare(
            CommandRequest(
                "engine-fit plan", model_path=model, fit_engines=[engine], out=tmp_path / "plan"
            )
        )
    assert not (tmp_path / "plan").exists()


def test_mlx_directory_uses_versioned_plan_and_cannot_be_relabelled(tmp_path, model):
    directory = tmp_path / "mlx-model"
    directory.mkdir()
    (directory / "config.json").write_text("{}")
    plan = prepare(
        CommandRequest(
            "engine-fit plan", model_path=directory, fit_engines=["mlx-lm"], out=tmp_path / "plan"
        )
    )
    assert plan["model"]["kind"] == "directory"
    changed = deepcopy(plan)
    changed["definition"] = "engine_fit_plan.v1"
    changed["plan_id"] = digest({k: v for k, v in changed.items() if k != "plan_id"})
    with pytest.raises(ContractError):
        validate_plan(changed)


def test_ollama_run_blocks_before_any_model_or_service_access(tmp_path, monkeypatch):
    from inferyard.runtime.engine_fit import execute

    monkeypatch.setattr("platform.system", lambda: "Darwin")
    with pytest.raises(PreflightError, match="ollama_service_idle_observation_unavailable"):
        execute(
            CommandRequest("engine-fit run", fit_engine="ollama", frozen_plan=tmp_path / "missing")
        )


def test_capability_cli_explains_ollama_gap_without_backend(capsys, monkeypatch):
    import json

    monkeypatch.setattr(
        "inferyard.platforms.engine_fit.host_identity",
        lambda: pytest.fail("offline capability listing queried native host"),
    )
    assert main(["engine-fit", "engines"]) == 0
    result = json.loads(capsys.readouterr().out)["details"]
    entries = {entry["id"]: entry for entry in result["engines"]}
    assert set(entries) == {"vllm", "sglang", "llama-cpp", "mlx-lm", "lmstudio", "ollama"}
    assert entries["ollama"]["execution"] == "blocked"
    assert entries["ollama"]["idle_source"] is None
    assert entries["mlx-lm"]["server_script"].endswith("data/engine_fit/mlx_server.py")
