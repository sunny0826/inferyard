"""Generate frozen configurations from discovered models, without preset model identity."""

import hashlib
import importlib.util
import json
import shutil
from pathlib import Path

import pytest

from inferyard.config.loader import load_config
from tests.unit.test_device_preflight import GIB, gguf, hardware, recommend


@pytest.fixture
def generator(tmp_path, monkeypatch):
    root = Path(__file__).parents[2]
    scripts = root / "scripts"
    monkeypatch.syspath_prepend(str(scripts))
    spec = importlib.util.spec_from_file_location(
        "windows_benchmark_config", scripts / "create_windows_benchmark_config.py"
    )
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    monkeypatch.setattr(module, "ROOT", tmp_path)
    monkeypatch.setattr(module, "require_destination", lambda path: path)
    (tmp_path / "bundles").mkdir()
    shutil.copyfile(root / "bundles/zh-smoke.json", tmp_path / "bundles/zh-smoke.json")
    model = gguf(tmp_path / "independent-model.gguf", "Independent model")
    engine = tmp_path / "engine.exe"
    engine.write_bytes(b"verified-engine-fixture")
    receipts = tmp_path / ".tools/downloads"
    receipts.mkdir(parents=True)
    (receipts / "windows-cuda-runtime.json").write_text(
        json.dumps(
            {
                "binary_path": str(engine),
                "binary_sha256": hashlib.sha256(engine.read_bytes()).hexdigest(),
                "release": "prism-b10743-adfffbe",
                "runtime_library_manifest": str(tmp_path / "manifest.json"),
            }
        )
    )
    report = tmp_path / "preflight.json"
    report.write_text(
        json.dumps(
            {
                "recommendation": recommend(
                    hardware(),
                    [
                        {
                            "path": str(model),
                            "name": "Independent model",
                            "status": "available",
                            "size_bytes": model.stat().st_size,
                            "context_length": 2048,
                        }
                    ],
                )
            }
        )
    )
    return module, report, model


def test_config_preserves_discovered_model_and_recommended_mode(generator, tmp_path):
    module, report, model = generator
    out = tmp_path / "chosen.toml"
    module.create(report, out)
    config = load_config(out).config.to_dict()
    assert config["model"]["local_path"] == str(model.resolve())
    assert config["model"]["display_name"] == "Independent model GGUF"
    assert config["model"]["revision"] == hashlib.sha256(model.read_bytes()).hexdigest()
    assert config["engine"]["backend"] == "cuda"
    assert config["engine"]["startup_args"][-2:] == ["--device", "CUDA2"]
    assert config["conditions"]["context_size"] == 2048
    assert config["output"]["min_available_memory_bytes"] >= GIB
    assert out.with_suffix(".chat-template.jinja").read_text() == "{{ messages }}"


def test_existing_template_is_preserved_and_no_partial_config_is_written(generator, tmp_path):
    module, report, _ = generator
    out = tmp_path / "chosen.toml"
    template = out.with_suffix(".chat-template.jinja")
    template.write_text("existing template")
    with pytest.raises(FileExistsError, match="chat_template_already_exists"):
        module.create(report, out)
    assert template.read_text() == "existing template" and not out.exists()
