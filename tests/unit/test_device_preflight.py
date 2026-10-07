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


def test_recommendation_uses_free_vram_model_size_and_selected_device():
    report = recommend(hardware(), models())
    selected = report["selected"]
    assert selected["mode"] == "cuda" and selected["gpu_index"] == 2
    assert selected["model_path"] == "other-model.gguf" and selected["context_size"] == 2048
    assert selected["min_available_memory_bytes"] >= GIB and selected["threads"] == 8


def test_busy_gpu_and_unavailable_driver_recommend_cpu_when_model_fits_host():
    state = hardware()
    state["gpu"]["devices"][0]["memory_free_bytes"] = GIB
    assert recommend(state, models())["selected"]["mode"] == "cpu"
    state["gpu"]["devices"] = []
    assert recommend(state, models())["selected"]["mode"] == "cpu"


@pytest.mark.parametrize("changed", ["memory", "disk", "models", "template"])
def test_resource_shortage_or_missing_model_cannot_be_recommended(changed):
    state, candidates = hardware(), models()
    if changed == "memory":
        state["memory_available_bytes"] = GIB
    elif changed == "disk":
        state["disk_free_bytes"] = GIB
    elif changed == "models":
        candidates = []
    else:
        candidates[0]["chat_template_available"] = False
    assert recommend(state, candidates)["status"] == "blocked"


def test_loaded_service_is_not_required_to_reserve_model_memory_a_second_time():
    state = hardware()
    state["memory_available_bytes"] = 2 * GIB
    state["gpu"]["devices"][0]["memory_free_bytes"] = GIB
    assert recommend(state, models(), model_loaded=True)["selected"]["mode"] == "cuda"


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


def test_model_discovery_is_not_bound_to_a_name_and_preserves_invalid_candidates(tmp_path):
    first = gguf(tmp_path / "a.gguf", "Alpha")
    second = gguf(tmp_path / "b.gguf", "Beta")
    (tmp_path / "invalid.gguf").write_bytes(b"not GGUF")
    found = discover_models([tmp_path])
    assert [m["name"] for m in found if m["status"] == "available"] == ["Alpha", "Beta"]
    assert found[-1]["status"] == "unavailable"
    assert discover_models([tmp_path], second)[0]["name"] == "Beta"
    assert read_metadata(first)["tokenizer.chat_template"] == "{{ messages }}"


def test_truncated_and_excessive_gguf_metadata_are_rejected(tmp_path):
    path = tmp_path / "malformed.gguf"
    path.write_bytes(b"GGUF" + struct.pack("<IQQ", 3, 1, 1) + struct.pack("<Q", 2**40))
    with pytest.raises(ValueError, match="too_large"):
        read_metadata(path)
    path.write_bytes(b"GGUF")
    with pytest.raises(ValueError):
        read_metadata(path)


@pytest.mark.parametrize(
    "key,value",
    [
        ("custom.context_length", True),
        ("custom.context_length", -1),
        ("custom.context_length", "2048"),
        ("general.file_type", float("nan")),
        ("tokenizer.chat_template", True),
    ],
)
def test_invalid_gguf_metadata_types_are_preserved_as_unavailable(
    tmp_path, monkeypatch, key, value
):
    from inferyard.platforms import device_preflight

    path = gguf(tmp_path / "bad-metadata.gguf")
    metadata = read_metadata(path)
    metadata[key] = value
    monkeypatch.setattr(device_preflight, "read_metadata", lambda _: metadata)
    found = discover_models([], path)
    assert found[0]["status"] == "unavailable"


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
