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
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, read_json
from inferyard.registry import adapter_factory
from inferyard.runtime.runner import execute_async
from tests.integration.test_runner import scenario  # noqa: F401, F811
from tests.lab_service import LabService, lab_config


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
    # Every canonical invocation goes through real CLI parsing/application dispatch.
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
    assert main(["verify", "--path", str(out)]) == 0
    capsys.readouterr()
    assert len(service.calls) == before
    html = (out / "report.html").read_text(encoding="utf-8")
    assert "北京" in html and config["engine"]["adapter"] in html
    # Freeze a real offline experiment; native Windows paths survive on this host.
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


@pytest.mark.parametrize("failure", ["truncated", "mismatched_id", "http_error"])
def test_probe_failure_preserves_full_denominator(lab_scenario, failure):
    command, deps, service = lab_scenario
    service.options[failure] = True
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 2
    data = read_trial(Path(result.evidence_dir))
    assert data["summary"]["counts"]["not_executed"] == len(
        command.config.bundle.to_dict()["cases"]
    )
    assert len(service.requests) == (2 if failure == "truncated" else 1)


def test_successful_response_without_release_keeps_dirty(lab_scenario):
    command, deps, service = lab_scenario
    # Become busy after the initial idle; release never follows the complete response.
    original = service.__call__

    def busy_after_send(request):
        result = original(request)
        if request.url.path == "/v1/chat/completions":
            service.options["busy"] = True
        return result

    engine = command.config.config.to_dict()["engine"]["adapter"]
    deps.adapter = lambda origin, secret: adapter_factory(engine)(
        origin, secret=secret, transport=httpx.MockTransport(busy_after_send)
    )
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 2
    assert read_json(locking.STATE_PATH)["dirty"] is True
    assert result.limitations
    assert len(service.requests) == 1
    read_trial(Path(result.evidence_dir))


def test_offline_rejects_altered_wire_even_without_manifest(lab_scenario):
    command, deps, _ = lab_scenario
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 0
    root = Path(result.evidence_dir)
    (root / "manifest.json").unlink()
    path = root / "events.jsonl"
    events = [json.loads(line) for line in path.read_text().splitlines()]
    event = next(e for e in events if e["event_type"] == "lab_wire")
    event["data"]["text"] = event["data"]["text"].replace("北京", "上海")
    path.write_text(
        "".join(json.dumps(e, ensure_ascii=False) + "\n" for e in events), encoding="utf-8"
    )
    with pytest.raises(EvidenceError, match="lab_wire_terminal_mismatch"):
        read_trial(root)


def test_frozen_plan_run_uses_shared_lab_lifecycle(lab_scenario, tmp_path, monkeypatch, capsys):
    import hashlib

    import inferyard.runtime.batch_runner as batch_runner
    import inferyard.runtime.trial_runner as trial_runner

    command, deps, service = lab_scenario
    config_path = tmp_path / "lab-plan.toml"
    write_config(command, config_path)
    loaded = load_config(config_path)
    exp = compile_single_plan(loaded.config.to_dict(), loaded.bundle.to_dict())["experiment"]
    for field, path in (("config", config_path), ("bundle", tmp_path / "bundle.json")):
        exp["workloads"][0][field] = {
            "path": path.name,
            "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
        }
    experiment = tmp_path / "source-experiment.json"
    experiment.write_text(json.dumps(exp), encoding="utf-8")
    assert main(["plan", "--experiment", str(experiment), "--out", str(tmp_path / "frozen")]) == 0
    capsys.readouterr()
    trial_deps = trial_runner.TrialDependencies(
        preflight=deps.preflight, adapter=deps.adapter, guard=deps.guard, sampler=deps.sampler
    )
    monkeypatch.setattr(batch_runner, "TrialDependencies", lambda **kwargs: trial_deps)
    monkeypatch.setattr(batch_runner, "adapter_factory", lambda _: deps.adapter)
    monkeypatch.setattr(trial_runner, "require_review", lambda bundle: None)
    assert (
        main(
            [
                "run",
                "--plan",
                str(tmp_path / "frozen/plan.json"),
                "--output-root",
                str(tmp_path / "batch"),
            ]
        )
        == 0
    )
    result = json.loads(capsys.readouterr().out)
    root = Path(result["details"]["runs"][0])
    data = read_trial(root)
    assert data["run"]["execution_mode"] == "experiment"
    assert data["summary"]["counts"]["completed"] == len(command.config.bundle.to_dict()["cases"])
    assert len(service.requests) == 5 + len(command.config.bundle.to_dict()["cases"])


@pytest.mark.parametrize(
    "artifact", ["budget", "parameters", "missing-budget", "missing-parameters"]
)
def test_offline_rechecks_lab_parameter_binding(lab_scenario, artifact):
    command, deps, _ = lab_scenario
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 0
    root = Path(result.evidence_dir)
    (root / "manifest.json").unlink()
    name = "token-budgets.v2.json" if "budget" in artifact else "effective-ordinary.json"
    path = root / name
    if artifact.startswith("missing-"):
        path.unlink()
    else:
        value = read_json(path)
        if artifact == "budget":
            value["entries"][0]["budget"]["lab_token_budget"]["server_instance_id"] = "2" * 32
        else:
            value["lab_parameters"]["generation"]["seed"] += 1
        path.write_text(json.dumps(value), encoding="utf-8")
    with pytest.raises(EvidenceError, match="lab_.*evidence_(invalid|missing)"):
        read_trial(root)


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
    assert read_json(locking.STATE_PATH)["dirty"] is False  # Actual release was observed.
