"""Official binding script output reaches real CLI config loading with mock engine transport."""

import json
import tomllib
from dataclasses import replace
from pathlib import Path

import inferyard.runtime.runner as running
from inferyard.cli import main
from inferyard.config.loader import load_config
from inferyard.contracts.validation import Document
from inferyard.evidence.ledger import read_trial
from tests.integration.test_lab_cli import lab_scenario, write_config  # noqa: F401
from tests.integration.test_native_cli import native_scenario as _native_scenario
from tests.integration.test_runner import scenario  # noqa: F401
from tests.unit.test_bind_service_config import bind
from tests.unit.test_bind_service_config import binder as _binder

native_scenario = _native_scenario
binder = _binder


def test_manifest_binding_script_to_cli_probe_round_trip(
    native_scenario, binder, tmp_path, monkeypatch, capsys
):
    command, deps, service = native_scenario
    module, observed = binder
    config = command.config.config.to_dict()
    config["engine"]["environment"] = {"CUDA_VISIBLE_DEVICES": "0", "CUDA_CACHE_DISABLE": "1"}
    config["engine"]["asset_manifest"].append(
        {"path": r"D:\lab\cuda.dll", "role": "library", "bytes": 123, "sha256": "c" * 64}
    )
    config["quantization_artifact_binding"] = {
        "root": r"D:\lab",
        "files": {"model": "artifact/model.gguf"},
    }
    command = replace(
        command, config=replace(command.config, config=Document.parse("config", config))
    )
    candidate = tmp_path / "candidate.toml"
    write_config(command, candidate)
    original_bytes = candidate.read_bytes()
    bound = tmp_path / "bound.toml"
    observed["ticks"] = config["endpoint"]["process_start_ticks"]
    original = bind(
        module, observed, candidate, bound, monkeypatch, pid=config["endpoint"]["server_pid"]
    )
    capsys.readouterr()
    parsed = tomllib.loads(bound.read_text(encoding="utf-8"))
    assert parsed == original
    Document.parse("config", parsed)
    assert load_config(bound).config.to_dict() == original
    assert parsed["engine"]["asset_manifest"] == config["engine"]["asset_manifest"]
    assert parsed["quantization_artifact_binding"] == config["quantization_artifact_binding"]
    assert candidate.read_bytes() == original_bytes
    monkeypatch.setattr(running, "Dependencies", lambda **kwargs: deps)
    assert main(["probe", "--config", str(bound)]) == 0
    root = Path(json.loads(capsys.readouterr().out)["evidence_dir"])
    assert (
        read_trial(root)["config"]["engine"]["asset_manifest"] == config["engine"]["asset_manifest"]
    )
    assert len(service.requests) == 2
