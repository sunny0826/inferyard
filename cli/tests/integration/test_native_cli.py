"""Canonical CLI supports release binaries without inventing lab IDs or drain proof."""

import asyncio
import json
from dataclasses import replace
from pathlib import Path

import httpx
import pytest

import inferyard.runtime.lock as locking
import inferyard.runtime.runner as running
from inferyard.cli import main
from inferyard.contracts.validation import Document
from inferyard.errors import EvidenceError
from inferyard.analysis.ledger import read_trial
from inferyard.evidence.storage import read_json
from inferyard.registry import adapter_factory
from inferyard.application.execution import execute_async
from tests.integration.test_lab_cli import lab_scenario, write_config  # noqa: F401
from tests.integration.test_runner import scenario  # noqa: F401
from tests.layout_mutations import change_observation
from tests.native_service import NativeService


@pytest.fixture
def native_scenario(lab_scenario):  # noqa: F811
    command, deps, _ = lab_scenario
    config = command.config.config.to_dict()
    config["engine"]["observation_mode"] = "auto"
    service = NativeService(config)
    deps.adapter = lambda origin, secret: adapter_factory(config["engine"]["adapter"])(
        origin, secret=secret, transport=httpx.MockTransport(service)
    )
    command = replace(
        command, config=replace(command.config, config=Document.parse("config", config))
    )
    return command, deps, service


def test_probe_native_run_report_verify(native_scenario, tmp_path, monkeypatch, capsys):
    command, deps, service = native_scenario
    monkeypatch.setattr(running, "Dependencies", lambda **kwargs: deps)
    path = tmp_path / "native.toml"
    write_config(command, path)
    assert main(["probe", "--config", str(path)]) == 0
    matrix = json.loads(capsys.readouterr().out)["details"]["engine_observation"]
    assert matrix["mode"] == "native"
    assert matrix["endpoints"]["/lab/v1/identity"]["available"] is None
    assert matrix["endpoints"]["/lab/v1/identity"]["missing_reason"] == "http_404"
    assert matrix["endpoints"]["/health"]["available"] is True
    assert main(["run", "--config", str(path)]) == 0
    root = Path(json.loads(capsys.readouterr().out)["evidence_dir"])
    data = read_trial(root)
    assert data["summary"]["counts"]["completed"] == data["summary"]["counts"]["planned"]
    lengths = [m for m in data["summary"]["metric_observations"] if m["metric_id"] == "X01"]
    assert lengths and all(m["source"] == "not_observed:native" for m in lengths)
    assert read_json(root / "manifest.json")["engine_observation"] == matrix
    assert read_json(locking.STATE_PATH)["completion_scope"] == "client_http"
    assert read_json(locking.STATE_PATH)["engine_internal_drain"] is None
    before = len(service.calls)
    out = tmp_path / "report"
    assert main(["report", "--runs", str(root), "--out", str(out)]) == 0
    capsys.readouterr()
    assert main(["verify", "--path", str(out)]) == 4
    capsys.readouterr()
    assert len(service.calls) == before
    document = read_json(out / "studio.json")
    assert document["runs"][0]["diagnostic"] is False
    assert {path.name for path in out.iterdir()} == {"studio.json"}


@pytest.mark.parametrize("phase", ["probe", "formal"])
@pytest.mark.parametrize(
    "fault,reason",
    [
        ("truncated", "lab_generation_done"),
        ("http_error", "http_error"),
        ("timeout", "total_timeout"),
    ],
)
def test_native_stop_reason_keeps_actual_failure_and_denominator(
    native_scenario, phase, fault, reason
):
    command, deps, service = native_scenario
    config = command.config.config.to_dict()
    config["execution"]["timeout_seconds"] = 0.01
    command = replace(
        command, config=replace(command.config, config=Document.parse("config", config))
    )
    first_formal = 3 + config["execution"]["warmup_count"]
    service.options["fail_at"] = first_formal if phase == "formal" else 2
    service.options["delay" if fault == "timeout" else fault] = 0.05 if fault == "timeout" else True
    code, result = asyncio.run(execute_async(command, deps))
    assert code == (3 if phase == "formal" else 2)
    assert "native_generation_failed:" + reason in result.limitations
    root = Path(result.evidence_dir)
    data = read_trial(root)
    counts = data["summary"]["counts"]
    assert counts["planned"] == sum(
        counts[s] for s in ("completed", "failed", "cancelled", "invalid", "not_executed")
    )
    assert counts["failed"] == (1 if phase == "formal" else 0)
    assert counts["not_executed"] == counts["planned"] - counts["failed"]
    assert read_json(locking.STATE_PATH)["dirty"] is True
    assert len(service.requests) == service.options["fail_at"]
