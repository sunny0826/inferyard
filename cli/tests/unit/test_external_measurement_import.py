import importlib
import json
import sys
from pathlib import Path

import pytest
from inferyard.errors import EvidenceError


@pytest.fixture
def importer(monkeypatch):
    monkeypatch.syspath_prepend(str(Path(__file__).resolve().parents[2] / "scripts"))
    return importlib.import_module("import_external_measurements")


def channel(code="E04", values=(90, 5)):
    return {
        "source": {
            "source_id": "fixture",
            "instrument": "simulated",
            "device_id": "fixture",
            "capture_method": "test",
            "clock_id": "clock",
            "scope": "component",
            "metric_id": code,
            "unit": {"E01": "bytes", "E02": "percent", "E04": "J"}[code],
            "calibration": {"status": "not_calibrated", "reason": "fixture_only"},
        },
        "max_gap_ns": 10,
        "counter_modulus": 100,
        "samples": [
            {"monotonic_ns": 10, "value": values[0], "missing_reason": None},
            {
                "monotonic_ns": 20,
                "value": values[1],
                "missing_reason": None,
                "wraps_since_previous": 1,
            },
        ],
    }


def test_missing_energy_and_zero_success_do_not_become_zero(importer):
    spec = channel(values=(None, None))
    for row in spec["samples"]:
        row["missing_reason"] = "permission_denied"
    result = importer.reduce_channel(spec, (10, 20), "clock", 0)
    assert result["value"] is None
    assert result["energy_per_successful_task"] is None
    assert result["E05_missing_reason"] == "no_successful_tasks"
