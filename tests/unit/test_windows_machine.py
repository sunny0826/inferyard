"""Inventory handles missing sensors, group topology and preserves benchmark conditions."""

import hashlib
import importlib.util
import json
import os
import struct
import subprocess
from copy import deepcopy
from pathlib import Path
from types import SimpleNamespace

import pytest

SPEC = importlib.util.spec_from_file_location(
    "windows_machine", Path(__file__).parents[2] / "scripts/collect_windows_machine.py"
)
machine = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(machine)


def entry(group, logical, core, efficiency, *, size=32):
    return struct.pack(
        "<IIIHBBBBBB", size, 0, 123, group, logical, core, 0, 0, efficiency, 5
    ) + bytes(size - 20)


def test_topology_walks_variable_sizes_and_preserves_group_identity():
    raw = entry(0, 0, 0, 1, size=40) + struct.pack("<II", 8, 999) + entry(1, 0, 0, 0)
    result = machine.cpu_summary(machine.parse_cpu_sets(raw))
    assert result["physical_cores"] == result["logical_processors"] == 2
    assert result["processor_groups"] == [0, 1]
    assert result["efficiency_classes"]["1"]["logical_processors"] == [[0, 0]]
    assert result["entries"][0]["parked"] and result["entries"][0]["allocated_to_current_process"]


@pytest.mark.parametrize(
    "raw", [b"x", struct.pack("<II", 0, 0), struct.pack("<II", 64, 0), struct.pack("<II", 8, 0)]
)
def test_malformed_topology_refuses_manufactured_values(raw):
    with pytest.raises(ValueError):
        machine.parse_cpu_sets(raw)


def test_power_unknowns_do_not_become_false_or_zero():
    result = machine.decode_power([255, 255, 255, 255, 0xFFFFFFFF, 0xFFFFFFFF])
    assert all(value is None for key, value in result.items() if key != "raw")
    assert (
        machine.decode_power([1, 128, 255, 0, 0xFFFFFFFF, 0xFFFFFFFF])["battery_present"] is False
    )


def test_timeout_preserves_completed_cim_queries(tmp_path, monkeypatch):
    early = {"processors": {"status": "ok", "source": "fixture", "data": [{"Name": "CPU"}]}}

    def timed_out(arguments, **kwargs):
        (tmp_path / "cim.raw.json").write_text(json.dumps(early))
        raise subprocess.TimeoutExpired("fixture", 1)

    monkeypatch.setattr(machine, "executable_command", timed_out)
    result = machine.cim_sections(tmp_path)
    assert result["processors"]["data"][0]["Name"] == "CPU"
    assert result["memory_modules"]["status"] == "unavailable"
    assert result["memory_modules"]["data"] is None
    assert result["memory_modules"]["missing_reason"] == "TimeoutExpired"


@pytest.mark.skipif(os.name != "nt", reason="D: Windows inventory integration")
def test_missing_optional_sensor_still_publishes_verified_inventory(tmp_path, monkeypatch):
    monkeypatch.setattr(machine, "ROOT", tmp_path)
    monkeypatch.setattr(
        machine,
        "cim_sections",
        lambda out: {
            name: {"source": "fixture", "status": "empty", "data": []}
            for name in machine.CIM_SECTIONS
        },
    )
    monkeypatch.setattr(
        machine,
        "cpu_sets",
        lambda: [
            {"group": 0, "core_index": 0, "logical_processor_index": 0, "efficiency_class": 0}
        ],
    )
    for name in ("native_features", "runtime_inventory", "dynamic_inventory", "memory_status"):
        monkeypatch.setattr(machine, name, lambda: {})
    monkeypatch.setattr(machine, "power_status", lambda: {"ac_online": None})
    monkeypatch.setattr(machine, "executable_command", lambda arguments: {"stdout": ""})

    def unavailable():
        raise PermissionError("sensor inaccessible")

    monkeypatch.setattr(machine, "nvidia_inventory", unavailable)
    out = tmp_path / "inventory"
    result = machine.collect_machine(out)
    inventory = json.loads((out / "machine.json").read_text(encoding="utf-8"))
    assert inventory["sections"]["nvidia"]["data"] is None
    assert "nvidia" in result["unavailable_sections"]
    assert "power_status.ac_online" in inventory["null_fields"]
    assert inventory["model_requests_sent"] == 0 and not inventory["hardware_qualification_added"]
    for name, expected in json.loads((out / "manifest.json").read_text())["sha256"].items():
        assert hashlib.sha256((out / name).read_bytes()).hexdigest() == expected
    with pytest.raises(FileExistsError):
        machine.collect_machine(out)


@pytest.mark.skipif(os.name != "nt", reason="Windows psutil and paths")
def test_reference_reports_shortfall_without_changing_candidate(tmp_path, monkeypatch):
    import psutil

    candidate = tmp_path / "candidate.toml"
    candidate.write_bytes(b"original-frozen-config")
    model = tmp_path / "model.gguf"
    model.write_bytes(b"model")
    config = {
        "model": {"local_path": str(model)},
        "engine": {"backend": "cpu"},
        "endpoint": {"url": "http://127.0.0.1:48857"},
        "output": {
            "root": str(tmp_path / "future-results"),
            "min_available_memory_bytes": 8 * 1024**3,
            "min_disk_bytes": 1,
        },
        "conditions": {
            "threads": 6,
            "threads_batch": 6,
            "context_size": 4096,
            "slots": 1,
            "ac_online": True,
        },
    }
    original = deepcopy(config)
    from inferyard.config.loader import loader as config_module

    monkeypatch.setattr(
        config_module,
        "load_config",
        lambda path: SimpleNamespace(
            source=candidate,
            config=SimpleNamespace(to_dict=lambda: config),
        ),
    )
    monkeypatch.setattr(psutil, "net_connections", lambda **kwargs: [])
    result = machine.candidate_reference(
        candidate,
        {
            "memory": {"data": {"PhysicalAvailable_bytes": 6 * 1024**3}},
            "background_load": {"data": {"collector_cpu_affinity": list(range(24))}},
            "power_status": {"data": {"ac_online": True}},
        },
    )
    assert not result["preparation_memory_sufficient"]
    assert result["memory_shortfall_bytes"] == 3 * 1024**3 + len(b"model")
    assert not result["budget_changed"] and config == original
    assert candidate.read_bytes() == b"original-frozen-config"
    assert not (tmp_path / "future-results").exists()


@pytest.mark.skipif(os.name != "nt", reason="Actual Windows API observations")
def test_native_cpu_memory_and_power_api_units():
    import psutil

    topology = machine.cpu_summary(machine.cpu_sets())
    memory = machine.memory_status()
    power = machine.power_status()
    assert topology["logical_processors"] == psutil.cpu_count()
    assert topology["physical_cores"] == psutil.cpu_count(logical=False)
    assert memory["PhysicalTotal_bytes"] == psutil.virtual_memory().total
    assert 0 <= memory["PhysicalAvailable_bytes"] <= memory["PhysicalTotal_bytes"]
    assert memory["CommitTotal_bytes"] <= memory["CommitLimit_bytes"]
    assert memory["page_size_bytes"] > 0
    assert power["ac_online"] in (None, True, False)
