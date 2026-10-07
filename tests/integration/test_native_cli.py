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
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.native_receipts import clean_receipt_name
from inferyard.evidence.storage import EvidenceError, read_json, read_jsonl
from inferyard.registry import adapter_factory
from inferyard.runtime.runner import execute_async
from tests.integration.test_lab_cli import lab_scenario, write_config  # noqa: F401
from tests.integration.test_lab_cli import (
    test_frozen_plan_run_uses_shared_lab_lifecycle as frozen_plan_flow,
)
from tests.integration.test_runner import scenario  # noqa: F401
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
    assert main(["verify", "--path", str(out)]) == 0
    capsys.readouterr()
    assert len(service.calls) == before
    html = (out / "report.html").read_text()
    assert "原生信号降级" in html and "进程级证据" in html and "内部全服务排空" in html
    assert "http_404" in html


@pytest.mark.parametrize("option", ["no_signals", "truncated", "old_dirty"])
def test_native_limits_and_dirty(native_scenario, option):
    command, deps, service = native_scenario
    service.options[option] = True
    if option == "old_dirty":
        service.options["slots"] = True
        with deps.lock() as lock:
            lock.dirty("old-run", command.config.config.to_dict()["endpoint"], "unfinished")
    code, result = asyncio.run(execute_async(command, deps))
    assert code == (0 if option == "no_signals" else 2)
    data = read_trial(Path(result.evidence_dir))
    assert data["summary"]["engine_observation"]["engine_internal_drain"]["value"] is None
    if option != "no_signals":
        assert read_json(locking.STATE_PATH)["dirty"] is True
        assert data["summary"]["counts"]["not_executed"] == data["summary"]["counts"]["planned"]
        assert len(service.requests) == (2 if option == "truncated" else 0)
    assert not any(
        c.method == "POST" and c.url.path != "/v1/chat/completions" for c in service.calls
    )


def test_native_offline_rejects_invented_drain(native_scenario):
    command, deps, _ = native_scenario
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 0
    root = Path(result.evidence_dir)
    (root / "manifest.json").unlink()
    path = root / "engine-capabilities.json"
    value = read_json(path)
    value["engine_internal_drain"]["value"] = True
    path.write_text(json.dumps(value))
    with pytest.raises(EvidenceError, match="cannot_prove_engine_drain"):
        read_trial(root)


def test_native_frozen_plan(native_scenario, tmp_path, monkeypatch, capsys):
    frozen_plan_flow(native_scenario, tmp_path, monkeypatch, capsys)


def test_native_explicit_lab_requirement_blocks_before_generation(native_scenario):
    command, deps, service = native_scenario
    config = command.config.config.to_dict()
    config["engine"]["observation_mode"] = "lab_required"
    command = replace(
        command, config=replace(command.config, config=Document.parse("config", config))
    )
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 2 and "required_lab_observation_unavailable" in result.limitations[0]
    assert service.requests == []


def test_native_cancel_disclosure_and_no_retry(native_scenario):
    command, deps, service = native_scenario
    service.options["truncated"] = True
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 2
    read_trial(Path(result.evidence_dir))
    events, _ = read_jsonl(Path(result.evidence_dir) / "events.jsonl")
    observations = [e["data"] for e in events if e["event_type"] == "native_observed"]
    assert observations[-1]["cancellation"]["client_action"] == "close_http_response_context"
    assert observations[-1]["cancellation"]["native_cancel"] is None
    assert len(service.requests) == 2  # One ordinary probe, one truncated stream; no retry.


def test_native_clean_basis_is_sealed_and_bound_to_completed_requests(native_scenario):
    command, deps, _ = native_scenario
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 0
    root = Path(result.evidence_dir)
    events, _ = read_jsonl(root / "events.jsonl")
    manifest = read_json(root / "manifest.json")
    finished = [e for e in events if e["event_type"] == "request_finished"]
    assert len(list(root.glob("client-http-clean-*.json"))) == len(finished)
    for event in finished:
        name = clean_receipt_name(event["request_id"])
        assert name in manifest["files"]
        receipt = read_json(root / name)
        assert receipt["run_id"] == event["run_id"]
        assert receipt["request_id"] == event["request_id"]
        assert receipt["completion_scope"] == "client_http"
        assert receipt["engine_internal_drain"] is None
    read_trial(root)


@pytest.mark.parametrize("change", ["scope", "client_id", "legacy", "sealed_missing"])
def test_native_clean_receipt_offline_rejection_and_legacy_reading(native_scenario, change):
    command, deps, _ = native_scenario
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 0
    root = Path(result.evidence_dir)
    if change == "sealed_missing":
        next(root.glob("client-http-clean-*.json")).unlink()
        with pytest.raises(EvidenceError, match="original_evidence_hash_mismatch"):
            read_trial(root)
        return
    (root / "manifest.json").unlink()
    if change == "legacy":
        for path in root.glob("client-http-clean-*.json"):
            path.unlink()
        assert read_trial(root)["summary"]["counts"]["completed"] > 0
        return
    path = next(root.glob("client-http-clean-*.json"))
    receipt = read_json(path)
    receipt["completion_scope" if change == "scope" else "client_request_id"] = "invented"
    path.write_text(json.dumps(receipt))
    with pytest.raises(EvidenceError, match="native_clean_receipt_binding_mismatch"):
        read_trial(root)


def test_native_receipt_io_failure_keeps_dirty(native_scenario, monkeypatch):
    command, deps, _ = native_scenario
    original = running.TrialJournal.snapshot

    def snapshot(self, name, value):
        if name.startswith("client-http-clean-"):
            raise EvidenceError("fixture_receipt_write_failed")
        return original(self, name, value)

    monkeypatch.setattr(running.TrialJournal, "snapshot", snapshot)
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 4 and "evidence_io_error" in result.limitations
    assert read_json(locking.STATE_PATH)["dirty"] is True
    assert list(Path(result.evidence_dir).glob("client-http-clean-*.json")) == []


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
