"""Local macOS preparation rejects stale capacity and does not reuse Linux identities."""

import importlib.util
import json
from pathlib import Path

import pytest

from inferyard.config.loader import load_config
from inferyard.platforms.device_preflight import discover_models, recommend
from tests.unit.test_device_preflight import GIB, gguf


@pytest.fixture
def generator(tmp_path, monkeypatch):
    path = Path(__file__).parents[2] / "scripts/create_macos_benchmark_config.py"
    spec = importlib.util.spec_from_file_location("macos_config_generator", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    monkeypatch.setattr(module.sys, "platform", "darwin")
    monkeypatch.setattr(module, "verify_engine", lambda path: "prism-b10743-adfffbe")
    hardware = {
        "platform": "Darwin",
        "apple_silicon": True,
        "cpu_model": "Apple M4",
        "memory_available_bytes": 12 * GIB,
        "memory_total_bytes": 16 * GIB,
        "logical_cpus": 10,
        "disk_free_bytes": 20 * GIB,
        "ac_online": True,
        "gpu": {"devices": [{"name": "Apple M4", "metal_supported": True}]},
    }
    monkeypatch.setattr(module, "hardware_snapshot", lambda path: hardware)
    monkeypatch.setattr(
        module, "environment_snapshot", lambda: {"platform": "Darwin", "profile": "balanced"}
    )
    model = gguf(tmp_path / "Mac-chosen-PQ2_0.gguf", "Mac selected model")
    engine = tmp_path / "llama-server"
    engine.write_bytes(b"synthetic engine, never executed")
    (tmp_path / "libggml-metal.dylib").write_bytes(b"synthetic Metal library")
    report = tmp_path / "preflight.json"
    report.write_text(
        json.dumps(
            {
                "hardware": hardware,
                "recommendation": recommend(hardware, discover_models([], model)),
            }
        )
    )
    return module, report, engine, hardware, model


def test_create_local_candidate_with_fresh_assets_and_no_requests(generator, tmp_path):
    module, report, engine, _, model = generator
    out = tmp_path / "macos.toml"
    result = module.create(report, engine, out)
    config = load_config(out).config.to_dict()
    assert config["engine"]["backend"] == "metal"
    assert config["model"]["local_path"] == str(model.resolve())
    assert config["conditions"]["threads"] == 5
    assert config["conditions"]["profile"] == "balanced"
    assert config["conditions"]["context_size"] == 4096
    assert config["endpoint"]["server_pid"] == config["endpoint"]["process_start_ticks"] == 1
    assert result["model_requests_sent"] == 0 and result["ready_to_run"] is False
    assert "libggml-metal.dylib" in json.loads(out.with_suffix(".libraries.json").read_text())
    before = out.read_bytes()
    with pytest.raises(FileExistsError):
        module.create(report, engine, out)
    assert out.read_bytes() == before


def test_candidate_checks_disk_on_existing_ancestor_before_creating_output(
    generator, tmp_path, monkeypatch
):
    module, report, engine, hardware, _ = generator
    out = tmp_path / "new" / "nested" / "macos.toml"

    def snapshot(path):
        assert path == tmp_path and path.is_dir()
        assert not out.parent.exists()
        return hardware

    monkeypatch.setattr(module, "hardware_snapshot", snapshot)
    module.create(report, engine, out)
    assert load_config(out).config.to_dict()["engine"]["backend"] == "metal"


@pytest.mark.parametrize("changed", ["memory", "ac", "backend", "library", "saved_platform"])
def test_candidate_rechecks_capacity_and_required_platform_assets(generator, tmp_path, changed):
    module, report, engine, hardware, _ = generator
    if changed == "memory":
        hardware["memory_available_bytes"] = GIB
    elif changed == "ac":
        hardware["ac_online"] = False
    elif changed == "backend":
        hardware["gpu"]["devices"] = []
    elif changed == "library":
        (tmp_path / "libggml-metal.dylib").unlink()
    else:
        original = json.loads(report.read_text())
        original["hardware"]["platform"] = "Linux"
        report.write_text(json.dumps(original))
    out = tmp_path / "blocked.toml"
    with pytest.raises(ValueError):
        module.create(report, engine, out)
    assert not out.exists() and not out.with_suffix(".chat-template.jinja").exists()
