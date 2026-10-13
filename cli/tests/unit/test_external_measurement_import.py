import importlib
import json
import sys
from pathlib import Path

import pytest

from inferyard.evidence.storage import EvidenceError


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


def test_explicit_single_wrap_is_recomputed_without_physical_qualification(importer):
    result = importer.reduce_channel(channel(), (10, 20), "clock", 3)
    assert result["value"] == 15
    assert result["energy_per_successful_task"] == 5
    assert not result["comparison_eligible"]
    assert not result["physical_calibration_verified"]


def test_counter_reset_without_wrap_is_unknown(importer):
    value = channel()
    value["samples"][1]["wraps_since_previous"] = 0
    result = importer.reduce_channel(value, (10, 20), "clock", 1)
    assert result["value"] is None
    assert "counter_reset_or_invalid_wrap" in result["reasons"]


@pytest.mark.parametrize(
    "field,value", [("clock_id", "wrong"), ("unit", "W"), ("calibration", None), ("scope", "model")]
)
def test_invalid_source_contract_is_rejected(importer, field, value):
    spec = channel()
    spec["source"][field] = value
    with pytest.raises(EvidenceError):
        importer.reduce_channel(spec, (10, 20), "clock", 1)


def test_missing_energy_and_zero_success_do_not_become_zero(importer):
    spec = channel(values=(None, None))
    for row in spec["samples"]:
        row["missing_reason"] = "permission_denied"
    result = importer.reduce_channel(spec, (10, 20), "clock", 0)
    assert result["value"] is None
    assert result["energy_per_successful_task"] is None
    assert result["E05_missing_reason"] == "no_successful_tasks"


def test_gap_blocks_energy_total(importer):
    spec = channel()
    spec["max_gap_ns"] = 5
    result = importer.reduce_channel(spec, (10, 20), "clock", 1)
    assert result["value"] is None
    assert "sample_gap_exceeds_declared_limit" in result["reasons"]


def test_duplicate_or_out_of_order_time_is_rejected(importer):
    spec = channel()
    spec["samples"][1]["monotonic_ns"] = 10
    with pytest.raises(EvidenceError, match="measurement_samples_not_ordered"):
        importer.reduce_channel(spec, (10, 20), "clock", 1)


def test_gpu_sample_mean_and_peak_have_observation_semantics(importer):
    mean = importer.reduce_channel(channel("E02", (20, 80)), (10, 20), "clock", 1)
    peak = importer.reduce_channel(channel("E01", (20, 80)), (10, 20), "clock", 1)
    assert mean["value"] == 50 and "not_time_average" in mean["statistic"]
    assert peak["value"] == 80 and "not_instantaneous_peak" in peak["statistic"]


def test_partial_run_retains_planned_denominator(importer):
    data = {
        "run": {"run_id": "run"},
        "requests": [
            {
                "t_send_ns": 10,
                "t_terminal_ns": 20,
                "clock_id": "clock",
                "execution_state": "completed",
                "quality_state": "pass",
            },
            {
                "t_send_ns": None,
                "t_terminal_ns": None,
                "clock_id": None,
                "execution_state": "not_executed",
                "quality_state": "not_scored",
            },
        ],
    }
    spec = {
        "definition": "external_measurements.v1",
        "run_id": "run",
        "window_ns": [10, 20],
        "channels": [channel()],
    }
    result = importer.build(spec, data)
    assert result["planned_requests"] == 2
    assert result["requests_with_bound_windows"] == 1
    assert result["successful_formal_tasks"] == 1
    assert not result["physical_measurement_qualification"]


def test_documented_calibration_does_not_mean_physically_verified(importer):
    spec = channel()
    spec["source"]["calibration"] = {
        "status": "documented",
        "performed_at": "2026-09-30T00:00:00Z",
        "uncertainty": 1,
        "uncertainty_unit": "J",
        "certificate_sha256": "a" * 64,
    }
    result = importer.reduce_channel(spec, (10, 20), "clock", 1)
    assert not result["physical_calibration_verified"]
    spec["source"]["calibration"]["performed_at"] = "2026-09-30T00:00:00"
    with pytest.raises(EvidenceError, match="measurement_calibration_date_requires_timezone"):
        importer.reduce_channel(spec, (10, 20), "clock", 1)


def test_report_escapes_imported_text(importer):
    text = importer.report({"instrument": "<script>alert(1)</script>"}).decode()
    assert "<script>" not in text and "&lt;script&gt;" in text


def test_failed_replay_never_writes_a_pass_marker(importer, tmp_path, monkeypatch):
    run = tmp_path / "run"
    run.mkdir()
    (run / "manifest.json").write_text("{}")
    data = {
        "run": {"run_id": "run"},
        "requests": [
            {
                "t_send_ns": 10,
                "t_terminal_ns": 20,
                "clock_id": "clock",
                "execution_state": "completed",
                "quality_state": "pass",
            }
        ],
    }
    source = tmp_path / "input.json"
    source.write_text(
        json.dumps(
            {
                "definition": "external_measurements.v1",
                "run_id": "run",
                "manifest_sha256": importer.sha256_file(run / "manifest.json"),
                "window_ns": [10, 20],
                "channels": [channel()],
            }
        )
    )
    out = tmp_path / "output"
    monkeypatch.setattr(importer, "read_trial", lambda _: data)

    def fail(_):
        raise EvidenceError("injected_offline_replay_failure")

    monkeypatch.setattr(importer, "verify_import", fail)
    monkeypatch.setattr(
        sys, "argv", ["import", "--spec", str(source), "--run", str(run), "--out", str(out)]
    )
    with pytest.raises(EvidenceError, match="injected_offline_replay_failure"):
        importer.main()
    assert (out / "artifact-hashes.json").exists()
    assert not (out / "verification.json").exists()
