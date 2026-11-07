"""One local synthetic plan executes all 12 slots and reads back as v2 slots."""

import asyncio
from collections.abc import Callable
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.application.types import CommandRequest
from inferyard.runtime import batch_state
from inferyard.runtime.batch_runner import execute_async
from tests.integration import comparison_plan_assertions as assertions
from tests.integration import comparison_plan_helpers as helpers
from tests.integration.test_runner import scenario

__all__ = ["scenario"]


@pytest.fixture(name="batch_scenario")
def batch_scenario(scenario: helpers.Scenario) -> helpers.Scenario:
    return scenario


@pytest.fixture(name="rig")
def rig_fixture() -> Callable[[str], SimpleNamespace]:
    import tests.local_engine_transport.conftest as local_engine_conftest

    return local_engine_conftest.rig.__wrapped__()


def test_twelve_slots_execute_with_handoffs_and_read_back(
    tmp_path: Path,
    batch_scenario: helpers.Scenario,
    rig: Callable[[str], SimpleNamespace],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _, deps, calls, _ = batch_scenario
    plan, sides, states, identities, identity_files = helpers.local_inputs(tmp_path, rig)
    slots, bindings = helpers.slot_inputs(plan, identities, tmp_path)
    assert len(slots) == 12
    helpers._assert_family_dispatch(sides)
    execution = helpers.SlotExecution(
        plan,
        states,
        identities,
        identity_files,
        bindings,
        deps,
        CommandRequest(
            "run",
            frozen_plan=tmp_path / "plan" / "plan.json",
            output_root=tmp_path / "batch",
        ),
        helpers.ExecutionCursor(
            states["a"],
            identities[("a", "core")],
            identity_files[("a", "core")],
            identities[("a", "core")]["binding"]["service"],
        ),
        {},
    )
    monkeypatch.setattr(batch_state, "process_start_ticks", execution.process_start)
    deps.preflight = execution.preflight
    deps.adapter = execution.adapter
    deps.safety = helpers.ControlledSafety
    execution.run_all()
    before = len(calls)
    code, again = asyncio.run(execute_async(execution.request, deps))
    assert code == 0 and again.status == "finished"
    assert len(calls) == before
    assert len(list((tmp_path / "batch" / "runs").iterdir())) == 12
    helpers.assert_read_back(tmp_path, slots)
    assertions.assert_drift_and_endpoint_rejection(tmp_path, execution.request, deps)
    assertions.assert_rejected_run_identity(tmp_path, plan, execution.request, deps)


