"""One synthetic plan executes all 12 frozen slots and reads back as v2 slots."""

import asyncio
from dataclasses import replace

import pytest

from inferyard.analysis.engine_comparison_v2 import read_comparison_v2
from inferyard.application.types import CommandRequest
from inferyard.config.comparison_plan import comparison_slots, freeze
from inferyard.platforms.identity import PreflightError
from inferyard.runtime import batch_state
from inferyard.runtime.batch_runner import execute_async
from tests.integration.test_runner import scenario


@pytest.fixture(name="batch_scenario")
def batch_scenario(scenario):
    return scenario
from tests.unit.test_comparison_plan import comparison_inputs


def test_twelve_slots_execute_with_handoffs_and_read_back(
    tmp_path, batch_scenario, monkeypatch
):
    fixture, deps, calls, _ = batch_scenario
    left, right, specs = comparison_inputs(
        tmp_path, runtime=fixture.config.config.to_dict()
    )
    plan = freeze(left, right, specs, destination=tmp_path / "plan")
    slots = comparison_slots(plan)
    assert len(slots) == 12

    alive = {}

    def process_start(pid):
        if pid in alive:
            return alive[pid]
        raise PreflightError("service_process_unavailable")

    monkeypatch.setattr(batch_state, "process_start_ticks", process_start)
    request = CommandRequest(
        "run",
        frozen_plan=tmp_path / "plan" / "plan.json",
        output_root=tmp_path / "batch",
    )
    for index, trial in enumerate(plan["trials"]):
        pid, ticks = 200_000 + index, 500 + index
        alive.clear()
        alive[pid] = ticks
        request = replace(
            request,
            workload_id=trial["workload_id"],
            endpoint_url="http://127.0.0.1:8080",
            server_pid=pid,
            handoff_note=None if index == 0 else "synthetic operator handoff",
        )
        code, result = asyncio.run(execute_async(request, deps))
        if index < len(plan["trials"]) - 1:
            assert code == 3 and result.status == "awaiting_handoff", result
            assert (
                result.details["next_trial"]["workload_id"]
                == plan["trials"][index + 1]["workload_id"]
            )
        else:
            assert code == 0 and result.status == "finished", result
    before = len(calls)
    code, again = asyncio.run(execute_async(request, deps))
    assert code == 0 and again.status == "finished"
    assert len(calls) == before
    assert len(list((tmp_path / "batch" / "runs").iterdir())) == 12

    document = read_comparison_v2(tmp_path / "plan", tmp_path / "batch")
    assert document["eligible"] is True, document["diagnostics"]
    assert document["run_set"]["expected_trials"] == [slot.trial_id for slot in slots]
    assert len(document["run_set"]["observed_runs"]) == 12
    assert document["run_set"]["missing"] == []
    assert document["run_set"]["unplanned"] == []
    assert document["history_stop"] is None
    assert {row["status"] for row in document["handoff"]["slots"]} == {"ok"}
    assert len(document["bundles"]) == 2
    assert all(
        len(group["left"]) == 3 and len(group["right"]) == 3
        for group in document["bundles"].values()
    )


def test_comparison_history_stop_blocks_a_new_call(tmp_path, batch_scenario, monkeypatch):
    fixture, deps, _, settings = batch_scenario
    left, right, specs = comparison_inputs(
        tmp_path, runtime=fixture.config.config.to_dict()
    )
    plan = freeze(left, right, specs, destination=tmp_path / "plan")
    alive = {200_001: 501}

    def process_start(pid):
        if pid in alive:
            return alive[pid]
        raise PreflightError("service_process_unavailable")

    monkeypatch.setattr(batch_state, "process_start_ticks", process_start)
    request = CommandRequest(
        "run",
        frozen_plan=tmp_path / "plan" / "plan.json",
        output_root=tmp_path / "batch",
    )
    first = replace(
        request,
        workload_id=plan["trials"][0]["workload_id"],
        endpoint_url="http://127.0.0.1:8080",
        server_pid=200_001,
    )
    settings["fail_index"] = 1
    code, result = asyncio.run(execute_async(first, deps))
    assert code == 2 and result.status == "blocked"
    code, stopped = asyncio.run(execute_async(replace(first, handoff_note=None), deps))
    assert code == 3 and stopped.status == "comparison_history_stopped"
    assert stopped.details["comparison_stop"]["reason"] == "comparison_history_failed"
    assert len(list((tmp_path / "batch" / "runs").iterdir())) == 1
    from pathlib import Path

    resume = CommandRequest("resume", from_run=Path(result.details["runs"][0]))
    code, blocked = asyncio.run(execute_async(resume, deps))
    assert code == 3 and blocked.status == "comparison_history_stopped"
