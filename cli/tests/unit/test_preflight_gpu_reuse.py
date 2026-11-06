"""Preflight observes the GPU only when CUDA admission needs it, once per call."""

import hashlib
import json

import pytest

from inferyard.platforms import device_preflight, identity


def test_cuda_preflight_observes_once_per_call_and_cpu_never(tmp_path, monkeypatch):
    model, engine, template, library = [
        tmp_path / name for name in ("model", "engine", "tpl", "lib")
    ]
    for path in (model, engine, template, library):
        path.write_bytes(path.name.encode())

    def digest(path):
        return hashlib.sha256(path.read_bytes()).hexdigest()

    manifest = tmp_path / "libraries.json"
    manifest.write_text(json.dumps({library.name: digest(library)}))
    config = {
        "model": {
            "local_path": str(model),
            "sha256": digest(model),
            "template_path": str(template),
            "template_sha256": digest(template),
        },
        "engine": {
            "backend": "cuda",
            "binary_path": str(engine),
            "binary_sha256": digest(engine),
            "runtime_library_manifest": str(manifest),
        },
        "endpoint": {"url": "http://127.0.0.1:48857"},
        "output": {"root": str(tmp_path), "min_available_memory_bytes": 1, "min_disk_bytes": 1},
        "conditions": {
            "ac_online": True,
            "profile": "fixture",
            "governor": "fixture",
            "epp": "fixture",
        },
    }
    monkeypatch.setattr(identity.platform, "system", lambda: "Linux")
    monkeypatch.setattr(identity, "verify_process", lambda *a, **k: {"process": "fixture"})
    monkeypatch.setattr(
        identity,
        "environment_snapshot",
        lambda: {**config["conditions"], "mem_available_bytes": 2**40},
    )
    monkeypatch.setattr(
        device_preflight,
        "nvidia_snapshot",
        lambda: {"status": "unavailable", "reason": "probe_timeout", "devices": []},
    )
    with pytest.raises(identity.PreflightError, match="cuda_device_unavailable"):
        identity.static_preflight(config)
    config["engine"]["backend"] = "cpu"
    result, _ = identity.static_preflight(config)
    assert "gpu" not in result["environment"]
