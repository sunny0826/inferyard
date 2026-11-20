"""Synthetic formal CLI chain; MockTransport does not confer Windows qualification."""

import asyncio
import json
from dataclasses import replace
from pathlib import Path

import httpx
import pytest

import inferyard.runtime.lock as locking
import inferyard.runtime.runner as running
from inferyard.cli import main
from inferyard.config.loader import load_config
from inferyard.config.single_plan import compile_single_plan
from inferyard.contracts.validation import Document
from inferyard.errors import EvidenceError
from inferyard.analysis.ledger import read_trial
from inferyard.evidence.storage import read_json
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.registry import adapter_factory
from inferyard.application.execution import execute_async
from tests.integration.test_runner import scenario  # noqa: F401, F811
from tests.lab_service import LabService, lab_config
from tests.layout_mutations import change_observation


def toml_value(value):
    if isinstance(value, dict):
        return "{" + ", ".join(f"{key} = {toml_value(item)}" for key, item in value.items()) + "}"
    if isinstance(value, list):
        return "[" + ", ".join(toml_value(item) for item in value) + "]"
    return json.dumps(value, ensure_ascii=False)


@pytest.fixture(params=["kvmem", "ninfer"])
def lab_scenario(scenario, request, tmp_path):  # noqa: F811
    command, deps, _, _ = scenario
    config = lab_config(command.config.config.to_dict(), request.param)
    service = LabService(config)
    deps.adapter = lambda origin, secret: adapter_factory(request.param)(
        origin, secret=secret, transport=httpx.MockTransport(service)
    )
    loaded = replace(command.config, config=Document.parse("config", config))
    command = replace(command, config=loaded)
    return command, deps, service


def write_config(command, path):
    config = command.config.config.to_dict()
    config["bundle"]["path"] = str(path.parent / "bundle.json")
    (path.parent / "bundle.json").write_text(
        json.dumps(command.config.bundle.to_dict()), encoding="utf-8"
    )
    lines = ["schema_version = 3"]
    for section, values in config.items():
        if isinstance(values, dict):
            lines.append(f"[{section}]")
            lines.extend(f"{key} = {toml_value(value)}" for key, value in values.items())
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return config


def test_probe_run_report_verify_and_offline_plan(lab_scenario, tmp_path, monkeypatch, capsys):
    command, deps, service = lab_scenario
    monkeypatch.setattr(running, "Dependencies", lambda **kwargs: deps)
    config_path = tmp_path / "lab.toml"
    config = write_config(command, config_path)
    assert main(["probe", "--config", str(config_path)]) == 0
    probe = json.loads(capsys.readouterr().out)
    probe_dir = Path(probe["evidence_dir"])
    assert read_trial(probe_dir)["summary"]["counts"]["not_executed"] == len(
        command.config.bundle.to_dict()["cases"]
    )
    assert main(["run", "--config", str(config_path)]) == 0
    result = json.loads(capsys.readouterr().out)
    run_dir = Path(result["evidence_dir"])
    data = read_trial(run_dir)
    assert data["config"]["engine"]["adapter"] == config["engine"]["adapter"]
    assert data["summary"]["counts"]["completed"] == data["summary"]["counts"]["planned"]
    assert all(row["actual_input_tokens"] == 1 for row in data["summary"]["input_lengths"]["bins"])
    assert read_json(locking.STATE_PATH)["dirty"] is False
    before = len(service.calls)
    out = tmp_path / "report"
    assert main(["report", "--runs", str(run_dir), "--out", str(out)]) == 0
    capsys.readouterr()
    assert main(["verify", "--path", str(out)]) == 4
    capsys.readouterr()
    assert len(service.calls) == before
    document = read_json(out / "studio.json")
    assert document["runs"][0]["profile"]["engine"]["adapter"] == config["engine"]["adapter"]
    assert {path.name for path in out.iterdir()} == {"studio.json"}
    loaded = load_config(config_path)
    exp = compile_single_plan(loaded.config.to_dict(), loaded.bundle.to_dict())["experiment"]
    for field, path in (("config", config_path), ("bundle", tmp_path / "bundle.json")):
        import hashlib

        exp["workloads"][0][field] = {
            "path": path.name,
            "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
        }
    experiment = tmp_path / "experiment.json"
    experiment.write_text(json.dumps(exp), encoding="utf-8")
    assert main(["plan", "--experiment", str(experiment), "--out", str(tmp_path / "plan")]) == 0
    capsys.readouterr()
    assert len(service.calls) == before
    assert config["model"]["local_path"] == loaded.config.to_dict()["model"]["local_path"]


def test_successful_response_without_release_keeps_dirty(lab_scenario, monkeypatch):
    command, deps, service = lab_scenario
    original = service.__call__

    def busy_after_send(request):
        result = original(request)
        if request.url.path == "/v1/chat/completions":
            service.options["busy"] = True
        return result

    engine = command.config.config.to_dict()["engine"]["adapter"]
    adapter_class = adapter_factory(engine)
    original_wait = adapter_class.wait_idle
    monkeypatch.setattr(
        adapter_class,
        "wait_idle",
        lambda self, seconds=5, observe=None: original_wait(
            self, min(seconds, 0.05), observe=observe
        ),
    )
    deps.adapter = lambda origin, secret: adapter_class(
        origin, secret=secret, transport=httpx.MockTransport(busy_after_send)
    )
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 2
    assert read_json(locking.STATE_PATH)["dirty"] is True
    assert result.limitations
    assert len(service.requests) == 1
    read_trial(Path(result.evidence_dir))


def test_formal_failure_stops_remaining_cases_without_retry(lab_scenario):
    command, deps, service = lab_scenario
    original = service.__call__

    def fail_first_formal(request):
        if request.url.path == "/v1/chat/completions" and len(service.requests) == 5:
            service.options["truncated"] = True
        return original(request)

    engine = command.config.config.to_dict()["engine"]["adapter"]
    deps.adapter = lambda origin, secret: adapter_factory(engine)(
        origin, secret=secret, transport=httpx.MockTransport(fail_first_formal)
    )
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 3
    data = read_trial(Path(result.evidence_dir))
    counts = data["summary"]["counts"]
    assert counts["failed"] == 1
    assert counts["not_executed"] == counts["planned"] - 1
    assert len(service.requests) == 6
    assert read_json(locking.STATE_PATH)["dirty"] is False
