"""Probe and formal CLI persist/replay usage without inventing missing counters."""

import json
from pathlib import Path

import pytest

import inferyard.runtime.lock as locking
import inferyard.runtime.runner as running
from inferyard.cli import main
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import read_json, read_jsonl
from tests.integration.test_lab_cli import lab_scenario, write_config  # noqa: F401
from tests.integration.test_native_cli import native_scenario as _native_scenario
from tests.integration.test_runner import scenario  # noqa: F401

native_scenario = _native_scenario


@pytest.mark.parametrize("phase", ["probe", "run"])
@pytest.mark.parametrize("mode", ["object", "null", "absent", "invalid"])
def test_cli_stream_usage(native_scenario, phase, mode, tmp_path, monkeypatch, capsys):
    command, deps, service = native_scenario
    monkeypatch.setattr(running, "Dependencies", lambda **kwargs: deps)
    service.options["usage_mode"] = mode
    if phase == "run":
        # Allow both preflight probes and warmups; exercise the first formal request.
        service.options["fail_at"] = (
            3 + command.config.config.to_dict()["execution"]["warmup_count"]
        )
    config_path = tmp_path / "native.toml"
    write_config(command, config_path)
    code = main([phase, "--config", str(config_path)])
    result = json.loads(capsys.readouterr().out)
    invalid = mode == "invalid"
    assert code == ((2 if phase == "probe" else 3) if invalid else 0)
    root = Path(result["evidence_dir"])
    data = read_trial(root)  # Offline wire replay checks the same usage evidence.
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
