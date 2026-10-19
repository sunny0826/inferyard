"""Cancellation and budget admission through the public run command."""

import asyncio

import pytest

from inferyard.runtime.batch_runner import execute_async
from tests.integration.context_speed_fixture import (  # noqa: F401
    allow_synthetic_review,
    tree_hashes,
)
from tests.integration.context_speed_fixture import (
    scenario as scenario_fixture,
)
from tests.integration.context_speed_fixture import speed_service as service_fixture
from tests.integration.test_context_speed_cli import invoke

speed_service = service_fixture
scenario = scenario_fixture


def prepare(service, monkeypatch, capsys, context=65536):
    allow_synthetic_review(monkeypatch)
    plan, batch = service["root"] / "plan", service["root"] / "batch"
    code, response = invoke(
        capsys,
        "plan",
        "--preset",
        "context-speed",
        "--config",
        service["write"](context),
        "--out",
        plan,
    )
    assert code == 0, response
    return plan, batch


def test_cancelled_formal_remains_readable_and_never_resumable(speed_service, monkeypatch, capsys):
    plan, batch = prepare(speed_service, monkeypatch, capsys)
    speed_service["formal_delay"] = 0.2

    async def cancel(request):
        task = asyncio.create_task(execute_async(request, speed_service["deps"]))
        while len(speed_service["requests"]) < 4 and not task.done():
            await asyncio.sleep(0.001)
        task.cancel()
        return await task

    monkeypatch.setattr(
        "inferyard.runtime.batch_runner.execute", lambda request: asyncio.run(cancel(request))
    )
    code, result = invoke(capsys, "run", "--plan", plan / "plan.json", "--output-root", batch)
    assert code == 130, result
    assert len(speed_service["requests"]) == 4
    before, calls = tree_hashes(batch), list(speed_service["calls"])
    code, result = invoke(capsys, "speed-results", "--batch", batch)
    assert code == 3, result
    assert result["details"]["execution"]["counts"]["cancelled"] == 1
    assert result["details"]["execution"]["counts"]["not_executed"] == 4
    assert result["details"]["summary"] is None
    assert before == tree_hashes(batch) and calls == speed_service["calls"]


@pytest.mark.parametrize("context", [65536, 131072, 262144])
def test_actual_template_budget_refuses_before_formal_request(
    speed_service, monkeypatch, capsys, context
):
    speed_service["formal_tokens"] = [context - 255] * 5
    plan, batch = prepare(speed_service, monkeypatch, capsys, context)
    code, result = invoke(capsys, "run", "--plan", plan / "plan.json", "--output-root", batch)
    assert code == 2, result
    assert not any(len(r["messages"][0]["content"]) > 1000 for r in speed_service["requests"])
    assert result["details"]["last_stop_reason"] != "plan_finished"
    before = tree_hashes(batch)
    code, result = invoke(capsys, "speed-results", "--batch", batch)
    # A real run with a not_executed formal row is a physically contradictory sample.
    assert code == 4 and result["details"]["read_state"] == "error"
    assert before == tree_hashes(batch)


def test_ordinary_batch_is_not_a_speed_result(tmp_path, scenario, capsys):
    from tests.integration.test_batch_runner import batch as ordinary_batch

    request, deps = ordinary_batch(tmp_path, scenario)
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0, result
    before = tree_hashes(request.output_root)
    sent = len(scenario[2])
    code, result = invoke(capsys, "speed-results", "--batch", request.output_root)
    assert code == 2
    assert result["details"]["error"]["code"] == "context_speed_batch_required"
    assert set(result["details"]) == {"schema_version", "kind", "read_state", "error"}
    assert before == tree_hashes(request.output_root) and len(scenario[2]) == sent
