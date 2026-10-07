"""Exercise real config loading, freezing and dispatch with simulated Windows engines."""

import hashlib
import json
from dataclasses import replace
from pathlib import Path

import pytest

import inferyard.runtime.batch_runner as batch_runner
import inferyard.runtime.runner as running
import inferyard.runtime.trial_runner as trial_runner
from inferyard.cli import main
from inferyard.config.loader import load_config
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.config.single_plan import compile_single_plan
from inferyard.contracts.validation import Document
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, read_json, read_jsonl, sha256_file
from tests.integration.test_lab_cli import lab_scenario, write_config  # noqa: F401
from tests.integration.test_native_cli import native_scenario as _native_scenario
from tests.integration.test_runner import scenario  # noqa: F401

native_scenario = _native_scenario


def with_warmups(command, count):
    config = command.config.config.to_dict()
    config["execution"]["warmup_count"] = count
    return replace(command, config=replace(command.config, config=Document.parse("config", config)))


def assert_frozen_evidence(root, count, *, probe=False):
    data = read_trial(root)
    assert data["config"]["execution"]["warmup_count"] == count
    manifest = read_json(root / "manifest.json")
    digest = sha256_file(root / "config.frozen.json")
    assert manifest["files"]["config.frozen.json"]["sha256"] == digest
    assert read_json(root / "selection.json")["config_sha256"] == digest
    plan = data["plan"]
    if not plan["runtime_bindings"]:
        assert plan["experiment"]["workloads"][0]["config"]["sha256"] == digest
    events, _ = read_jsonl(root / "events.jsonl")
    phases = [e["phase"] for e in events if e["event_type"] == "request_started"]
    assert phases.count("probe") == 2
    assert phases.count("warmup") == (0 if probe else count)
    counts = data["summary"]["counts"]
    assert phases.count("formal") == (0 if probe else counts["planned"])
    assert counts["not_executed" if probe else "completed"] == counts["planned"]


@pytest.mark.parametrize("command_name", ["probe", "run"])
@pytest.mark.parametrize("count", [0, 3])
def test_cli_loads_zero_or_three_and_seals_actual_warmups(
    native_scenario, command_name, count, tmp_path, monkeypatch, capsys
):
    command, deps, service = native_scenario
    command = with_warmups(command, count)
    monkeypatch.setattr(running, "Dependencies", lambda **kwargs: deps)
    path = tmp_path / "warmups.toml"
    write_config(command, path)
    assert main([command_name, "--config", str(path)]) == 0
    root = Path(json.loads(capsys.readouterr().out)["evidence_dir"])
    assert_frozen_evidence(root, count, probe=command_name == "probe")
    cases = len(command.config.bundle.to_dict()["cases"])
    expected = 2 if command_name == "probe" else 2 + count + cases
    assert len(service.requests) == expected


@pytest.mark.parametrize("command_name", ["probe", "run"])
@pytest.mark.parametrize("count", [-1, 4, True, "0"])
def test_cli_rejects_invalid_warmups_before_transport(
    native_scenario, command_name, count, tmp_path, monkeypatch, capsys
):
    command, deps, service = native_scenario
    monkeypatch.setattr(running, "Dependencies", lambda **kwargs: deps)
    path = tmp_path / "invalid-warmups.toml"
    write_config(command, path)
    path.write_text(
        path.read_text().replace("warmup_count = 3", "warmup_count = " + json.dumps(count)),
        encoding="utf-8",
    )
    assert main([command_name, "--config", str(path)]) == 2
    output = capsys.readouterr()
    assert "config_input.execution.warmup_count" in output.out + output.err
    assert service.calls == []


@pytest.mark.parametrize("count", [0, 3])
def test_frozen_plan_keeps_warmup_choice_and_rejects_later_mutation(
    native_scenario, count, tmp_path, monkeypatch, capsys
):
    command, deps, service = native_scenario
    command = with_warmups(command, count)
    path = tmp_path / "warmup-plan.toml"
    write_config(command, path)
    loaded = load_config(path)
    exp = compile_single_plan(loaded.config.to_dict(), loaded.bundle.to_dict())["experiment"]
    for field, source in (("config", path), ("bundle", tmp_path / "bundle.json")):
        exp["workloads"][0][field] = {
            "path": source.name,
            "sha256": hashlib.sha256(source.read_bytes()).hexdigest(),
        }
    experiment = tmp_path / "experiment.json"
    experiment.write_text(json.dumps(exp), encoding="utf-8")
    frozen = tmp_path / "frozen"
    assert main(["plan", "--experiment", str(experiment), "--out", str(frozen)]) == 0
    capsys.readouterr()
    plan = read_json(frozen / "plan.json")
    binding = plan["runtime_bindings"][0]["config"]
    snapshot = frozen / binding["path"]
    assert read_json(snapshot)["execution"]["warmup_count"] == count
    assert sha256_file(snapshot) == binding["sha256"]
    deps_for_trial = trial_runner.TrialDependencies(
        preflight=deps.preflight, adapter=deps.adapter, guard=deps.guard, sampler=deps.sampler
    )
    monkeypatch.setattr(batch_runner, "TrialDependencies", lambda **kwargs: deps_for_trial)
    monkeypatch.setattr(batch_runner, "adapter_factory", lambda _: deps.adapter)
    monkeypatch.setattr(trial_runner, "require_review", lambda bundle: None)
    args = ["run", "--plan", str(frozen / "plan.json"), "--output-root", str(tmp_path / "batch")]
    assert main(args) == 0
    result = json.loads(capsys.readouterr().out)
    root = Path(result["details"]["runs"][0])
    assert_frozen_evidence(root, count)
    before = len(service.calls)
    changed = read_json(snapshot)
    changed["execution"]["warmup_count"] = 3 if count == 0 else 0
    snapshot.write_text(json.dumps(changed), encoding="utf-8")
    with pytest.raises(EvidenceError, match="plan_source_hash_mismatch"):
        read_frozen_plan(frozen / "plan.json")
    args[-1] = str(tmp_path / "changed-batch")
    assert main(args) == 4
    capsys.readouterr()
    assert len(service.calls) == before
