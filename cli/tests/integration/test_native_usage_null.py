"""Probe and formal CLI persist/replay usage without inventing missing counters."""

import json
from dataclasses import replace
from pathlib import Path

import httpx
import pytest

import inferyard.runtime.lock as locking
import inferyard.runtime.runner as running
from inferyard.analysis.ledger import read_trial
from inferyard.cli import main
from inferyard.contracts.validation import Document
from inferyard.evidence.storage import read_json, read_jsonl
from inferyard.registry import adapter_factory
from tests.integration.test_runner import scenario  # noqa: F401
from tests.lab_service import lab_config
from tests.native_service import NativeService


def _toml_value(value):
    if isinstance(value, dict):
        return "{" + ", ".join(f"{key} = {_toml_value(item)}" for key, item in value.items()) + "}"
    if isinstance(value, list):
        return "[" + ", ".join(_toml_value(item) for item in value) + "]"
    return json.dumps(value, ensure_ascii=False)


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
            lines.extend(f"{key} = {_toml_value(value)}" for key, value in values.items())
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return config


@pytest.fixture(params=["kvmem", "ninfer"])
def native_scenario(scenario, request):  # noqa: F811
    command, deps, _, _ = scenario
    config = lab_config(command.config.config.to_dict(), request.param)
    config["engine"]["observation_mode"] = "auto"
    service = NativeService(config)
    deps.adapter = lambda origin, secret: adapter_factory(request.param)(
        origin, secret=secret, transport=httpx.MockTransport(service)
    )
    command = replace(
        command, config=replace(command.config, config=Document.parse("config", config))
    )
    return command, deps, service


@pytest.mark.parametrize("phase", ["probe", "run"])
@pytest.mark.parametrize("mode", ["object", "null", "absent", "invalid"])
def test_cli_stream_usage(native_scenario, phase, mode, tmp_path, monkeypatch, capsys):
    command, deps, service = native_scenario
    monkeypatch.setattr(running, "Dependencies", lambda **kwargs: deps)
    service.options["usage_mode"] = mode
    if phase == "run":
        service.options["fail_at"] = (
            1 + command.config.config.to_dict()["execution"]["warmup_count"]
        )
    config_path = tmp_path / "native.toml"
    write_config(command, config_path)
    code = main([phase, "--config", str(config_path)])
    result = json.loads(capsys.readouterr().out)
    invalid = mode == "invalid"
    assert code == ((2 if phase == "probe" else 3) if invalid else 0)
    root = Path(result["evidence_dir"])
    data = read_trial(root)
    events, _ = read_jsonl(root / "events.jsonl")
    target = next(
        e
        for e in events
        if e["event_type"] == "request_started"
        and (e["phase"] == "formal" if phase == "run" else e["data"]["body"]["stream"])
    )
    key = target["request_id"]
    terminal = next(
        e["data"]
        for e in events
        if e["request_id"] == key and e["event_type"] == "request_finished"
    )
    assert terminal["execution_state"] == ("failed" if invalid else "completed")
    assert read_json(locking.STATE_PATH)["dirty"] is invalid
    assert all(
        b["max_tokens"] == command.config.config.to_dict()["generation"]["max_tokens"]
        for b in service.requests
    )
    assert all(
        b["stream_options"] == {"include_usage": True} for b in service.requests if b["stream"]
    )
    if invalid:
        assert "native_generation_failed:lab_generation_usage" in result["limitations"]
        assert terminal["error_category"] == "lab_generation_usage"
        assert len(service.requests) == (2 if phase == "probe" else service.options["fail_at"])
    else:
        usage = next(
            e["data"] for e in events if e["request_id"] == key and e["event_type"] == "lab_usage"
        )
        reported = mode == "object"
        assert usage["prompt_tokens"] == (7 if reported else None)
        assert (
            usage["completion_tokens"] == terminal["completion_tokens"] == (2 if reported else None)
        )
        assert usage["usage_missing_reasons"]["prompt_tokens"] == (
            None if reported else "not_reported"
        )
        assert usage["usage_missing_reasons"]["completion_tokens"] == (
            None if reported else "not_reported"
        )
    counts = data["summary"]["counts"]
    assert counts["planned"] == sum(
        counts[s] for s in ("completed", "failed", "cancelled", "invalid", "not_executed")
    )
    if phase == "run" and not invalid:
        assert counts["completed"] == counts["planned"]
