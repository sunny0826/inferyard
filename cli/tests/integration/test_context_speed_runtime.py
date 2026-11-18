"""Cancellation and budget admission through the public run command."""

import asyncio

import pytest

from inferyard.application.execution import execute_async
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
        "inferyard.application.execution.execute", lambda request: asyncio.run(cancel(request))
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
