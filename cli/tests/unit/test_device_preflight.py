"""Hardware-based mode selection and bounded discovery of independent local models."""

import struct
from copy import deepcopy

import pytest

from inferyard.platforms.device_preflight import GIB, discover_models, recommend
from inferyard.platforms.gguf_metadata import read_metadata


def gguf(path, name="Different local model"):
    def string(text):
        raw = text.encode()
        return struct.pack("<Q", len(raw)) + raw

    values = {
        "general.name": name,
        "general.architecture": "custom",
        "tokenizer.chat_template": "{{ messages }}",
    }
    raw = b"GGUF" + struct.pack("<IQQ", 3, 1, len(values))
    for key, value in values.items():
        raw += string(key) + struct.pack("<I", 8) + string(value)
    path.write_bytes(raw)
    return path


def hardware():
    return {
        "memory_available_bytes": 12 * GIB,
        "logical_cpus": 16,
        "disk_free_bytes": 40 * GIB,
        "gpu": {
            "devices": [
                {
                    "index": 2,
                    "name": "GPU selected from observations",
                    "memory_total_bytes": 8 * GIB,
                    "memory_free_bytes": 7 * GIB,
                }
            ]
        },
    }


def models():
    return [
        {
            "path": "other-model.gguf",
            "name": "Any text model",
            "size_bytes": 2 * GIB,
            "context_length": 2048,
            "status": "available",
        }
    ]


def test_metal_capacity_uses_shared_host_memory_without_fabricating_vram():
    state = hardware()
    state.update(platform="Darwin", apple_silicon=True)
    state["gpu"]["devices"] = [
        {"name": "Apple M4", "metal_supported": True, "memory_kind": "shared_host"}
    ]
    selected = recommend(state, models())["selected"]
    assert selected["mode"] == "metal"
    assert selected["gpu_name"] == "Apple M4"
    assert selected["gpu_index"] is None and selected["estimated_gpu_memory_bytes"] is None
    state["memory_available_bytes"] = 3 * GIB
    assert recommend(state, models())["reason"] == "insufficient_host_memory"
    state["memory_available_bytes"] = 12 * GIB
    state["gpu"]["devices"][0]["metal_supported"] = None
    assert recommend(state, models())["selected"]["mode"] == "cpu"


def test_device_check_cli_is_read_only_and_selects_a_local_model(tmp_path, monkeypatch, capsys):
    import json

    from inferyard.cli import main
    from inferyard.platforms import device_preflight

    path = gguf(tmp_path / "user-chosen.gguf")
    before = path.read_bytes()
    monkeypatch.setattr(device_preflight, "hardware_snapshot", lambda root: deepcopy(hardware()))
    assert main(["device-check", "--models-root", str(tmp_path), "--json"]) == 0
    result = json.loads(capsys.readouterr().out)
    assert result["details"]["model_requests_sent"] == 0
    assert result["details"]["recommendation"]["selected"]["model_path"] == str(path.resolve())
    assert path.read_bytes() == before
