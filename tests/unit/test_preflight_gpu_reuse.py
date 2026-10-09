"""CUDA admission and advice use one observation within each preflight."""

import hashlib
import json

import pytest

from inferyard.platforms import device_preflight, identity


def test_cuda_preflight_reuses_observation_but_refreshes_next_call(tmp_path, monkeypatch):
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
    monkeypatch.setattr(identity, "verify_process", lambda *a: {"process": "fixture"})
    monkeypatch.setattr(
        identity,
        "environment_snapshot",
        lambda: {**config["conditions"], "mem_available_bytes": 2**40},
    )
    calls = []

    def gpu():
        value = {
            "status": "observed",
            "source": "nvidia-smi",
            "devices": [{"name": f"GPU-{len(calls)}"}],
        }
        calls.append(value)
        return value

    monkeypatch.setattr(device_preflight, "nvidia_snapshot", gpu)
    for expected in range(2):
        result, _ = identity.static_preflight(config)
        assert len(calls) == expected + 1
        assert result["environment"]["gpu"]["devices"] == calls[-1]["devices"]
        assert result["device_preflight"]["hardware"]["gpu"] == calls[-1]
    monkeypatch.setattr(
        device_preflight,
        "nvidia_snapshot",
        lambda: {"status": "unavailable", "reason": "probe_timeout", "devices": []},
    )
    with pytest.raises(identity.PreflightError, match="cuda_device_unavailable"):
        identity.static_preflight(config)


def test_advice_preserves_unavailable_injected_observation(tmp_path, monkeypatch):
    monkeypatch.setattr(device_preflight.platform, "system", lambda: "Linux")
    observed = {"status": "unavailable", "reason": "probe_timeout", "devices": []}

    def forbidden():
        pytest.fail("injected observation must not trigger a second GPU query")

    monkeypatch.setattr(device_preflight, "nvidia_snapshot", forbidden)
    result = device_preflight.hardware_snapshot(tmp_path, {}, gpu_observation=observed)
    assert result["gpu"] == observed
    assert result["missing"]["gpu"] == "probe_timeout"
