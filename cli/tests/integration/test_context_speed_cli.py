"""Public parser/dispatch and real localhost HTTP; host/review are simulated."""

import json

import pytest

from inferyard.cli import main
from tests.integration.context_speed_fixture import (
    allow_synthetic_review,
    scenario,  # noqa: F401
    tree_hashes,
)
from tests.integration.context_speed_fixture import (
    speed_service as service_fixture,
)

speed_service = service_fixture


def invoke(capsys, *args):
    code = main(list(map(str, args)))
    captured = capsys.readouterr()
    invoke.last_stderr = captured.err
    return code, json.loads(captured.out)


def run_speed(service, monkeypatch, capsys, context=65536):
    allow_synthetic_review(monkeypatch)
    path = service["write"](context)
    plan, batch = service["root"] / "plan", service["root"] / "batch"
    code, response = invoke(
        capsys, "plan", "--preset", "context-speed", "--config", path, "--out", plan
    )
    assert code == 0, (response, invoke.last_stderr)
    code, response = invoke(capsys, "run", "--plan", plan / "plan.json", "--output-root", batch)
    assert code == 0, (response, invoke.last_stderr)
    return batch, response


@pytest.mark.parametrize("context", [65536, 131072, 262144])
def test_public_five_trials_and_readonly_results(speed_service, monkeypatch, capsys, context):
    batch, execution = run_speed(speed_service, monkeypatch, capsys, context)
    assert len(speed_service["requests"]) == 20
    assert len(execution["details"]["runs"]) == 5
    assert "repetition_metrics" not in execution["details"]
    before = tree_hashes(batch)
    calls = list(speed_service["calls"])
    code, result = invoke(capsys, "speed-results", "--batch", batch)
    assert code == 0, result
    details = result["details"]
    assert details["execution"]["counts"]["completed"] == 5
    assert details["measurement"]["valid_count"] == 5
    assert details["summary"]["columns"]["time"] == {"median": 0.4, "min": 0.4, "max": 0.4}
    assert details["summary"]["columns"]["prefill"]["median"] == 250
    assert details["summary"]["columns"]["decode"]["median"] == 510
    assert all(s["columns"]["context"]["value"] == context for s in details["samples"])
    assert before == tree_hashes(batch)
    assert calls == speed_service["calls"]
    assert not list(batch.rglob("*.html"))


def test_actual_public_review_gate(speed_service, capsys):
    path = speed_service["write"]()
    out = speed_service["root"] / "refused"
    assert invoke(capsys, "init", "--bundle", "zh-context-speed", "--out", out)[0] != 0
    assert not out.exists()
    assert (
        invoke(capsys, "plan", "--preset", "context-speed", "--config", path, "--out", out)[0] == 2
    )
    assert not out.exists()
    assert speed_service["calls"] == []
