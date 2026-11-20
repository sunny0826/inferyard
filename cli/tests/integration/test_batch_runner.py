"""Public plan/resume flows keep frozen inputs, failure history and service boundaries."""

import asyncio
import copy
import hashlib
import json
from dataclasses import dataclass, replace
from pathlib import Path

import pytest

from inferyard.analysis.scoring import score_case
from inferyard.application.types import CommandRequest
from inferyard.cli import main
from inferyard.config.planning import write_plan
from inferyard.contracts.validation import Document, json_bytes
from inferyard.errors import EvidenceError, PreflightError
from inferyard.analysis.ledger import read_trial
from inferyard.evidence.storage import sha256_file
from inferyard.runtime.runner import Dependencies
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.application.execution import execute_async
from tests.integration.test_runner import scenario as runner_scenario
from tests.unit.phase2_contract_helpers import experiment

scenario = runner_scenario

type JsonScalar = str | int | float | bool | None
type JsonNode = JsonScalar | list[JsonNode] | dict[str, JsonNode]
type JsonObject = dict[str, JsonNode]
type Scenario = tuple[CommandRequest, Dependencies, list[JsonObject], JsonObject]


@dataclass(frozen=True)
class BatchOptions:
    second: bool = False
    duration_limit: int | None = None
    capacity: bool = False
    performance_policy: dict[str, int | float] | None = None
    goodput_thresholds: dict[str, str | int | float] | None = None


def _apply_definition_options(definition: JsonObject, options: BatchOptions) -> None:
    if options.performance_policy is not None:
        definition["performance_environment"] = options.performance_policy
    if options.goodput_thresholds is not None:
        definition["goodput_thresholds"] = options.goodput_thresholds


def batch(
    tmp_path: Path, scenario: Scenario, options: BatchOptions | None = None
) -> tuple[CommandRequest, Dependencies]:
    options = options or BatchOptions()
    second, duration_limit = options.second, options.duration_limit
    request, deps, _, _ = scenario
    deps.scorer = score_case
    inputs = tmp_path / "inputs"
    inputs.mkdir()
    config, corpus = request.config.config.to_dict(), request.config.bundle.to_dict()
    config["bundle"]["path"] = "bundle.json"
    definition = experiment()
    _apply_definition_options(definition, options)
    definition["definition_versions"] = dict.fromkeys(
        ["measurement", "scoring", "comparison"], "phase2.v1"
    )
    definition["budget"]["max_wall_seconds"] = 100_000
    workload = definition["workloads"][0]
    workload["timeout_seconds"] = config["execution"]["timeout_seconds"]
    workload["protocol"]["case_ids"] = [c["case_id"] for c in corpus["cases"]]
    if duration_limit is not None:
        workload["purpose"] = "stability"
        workload["repeats"] = 1
        duration_seconds = 2 * workload["timeout_seconds"] if duration_limit == 1 else 0.5
        workload["protocol"].update(
            kind="duration",
            duration_seconds=duration_seconds,
            max_requests=duration_limit,
            window_seconds=duration_seconds,
            min_completed_per_case_per_window=1,
            drain_timeout_seconds=workload["timeout_seconds"],
        )
    if options.capacity:
        definition["capacity_stop"] = "first_failed_request"
        workload.update(purpose="performance", input_target_tokens=1)
    if second:
        workload["repeats"] = 1
    for kind, data in [("config", config), ("bundle", corpus)]:
        raw = json_bytes(data)
        (inputs / f"{kind}.json").write_bytes(raw)
        workload[kind] = dict(path=f"{kind}.json", sha256=hashlib.sha256(raw).hexdigest())
    if second:
        definition["workloads"].append({**copy.deepcopy(workload), "workload_id": "w2"})
    source = inputs / "experiment.json"
    source.write_bytes(json_bytes(definition))
    frozen = tmp_path / "frozen"
    write_plan(source, frozen)
    return CommandRequest(
        "run", frozen_plan=frozen / "plan.json", output_root=tmp_path / "batch", diagnostic=True
    ), deps


def test_cancel_resume_only_unexecuted_then_next_planned_repetitions(
    tmp_path, scenario, monkeypatch
):
    request, deps = batch(tmp_path, scenario)

    async def cancel():
        task = asyncio.create_task(execute_async(request, deps))
        while len(scenario[2]) < 6 and not task.done():
            await asyncio.sleep(0.001)
        task.cancel()
        return await task

    code, result = asyncio.run(cancel())
    assert code == 130 and len(scenario[2]) == 6
    parent_path = Path(result.details["runs"][0])
    hashes = {p.name: sha256_file(p) for p in parent_path.iterdir()}
    from inferyard.application.verification import execute as verify
    from inferyard.runtime import batch_state

    with monkeypatch.context() as patch:
        patch.setattr(
            batch_state, "tool_source_hash", lambda: pytest.fail("offline current source")
        )
        patch.setattr(batch_state, "open_batch", lambda *a, **k: pytest.fail("offline admission"))
        checked_code, checked = verify(CommandRequest("verify", run=request.output_root))
    assert checked_code == 0 and checked.status == "verified"
    assert checked.completeness == "incomplete"
    assert checked.details["sealed"] is False
    assert len(checked.details["missing_trials"]) == 2
    assert checked.details["trials"][0]["sealed"]
    assert checked.details["trials"][0]["execution_completeness"] == "incomplete"
    assert checked.details["trials"][0]["stop_reason"] == "user_cancelled"
    manifest = parent_path / "manifest.json"
    raw_manifest = manifest.read_bytes()
    manifest.unlink()
    assert verify(CommandRequest("verify", run=request.output_root))[0] == 3
    manifest.write_bytes(raw_manifest)
    code, blocked = asyncio.run(execute_async(request, deps))
    assert code == 3 and blocked.status == "resume_required" and len(scenario[2]) == 6
    resume = CommandRequest("resume", from_run=parent_path)
    code, resumed = asyncio.run(execute_async(resume, deps))
    assert code == 0 and resumed.status == "resumed_subset"
    assert len(scenario[2]) == 13
    child = read_trial(Path(resumed.details["runs"][-1]))
    assert child["selection"]["case_ids"] == read_trial(parent_path)["selection"]["case_ids"][1:]
    assert child["summary"]["scope_complete"] and child["summary"]["completeness"] == "incomplete"
    assert hashes == {p.name: sha256_file(p) for p in parent_path.iterdir()}
    with pytest.raises(PreflightError, match="latest_unfinished"):
        asyncio.run(execute_async(resume, deps))
    code, done = asyncio.run(execute_async(request, deps))
    assert code == 0 and len(scenario[2]) == 29
    assert len(done.details["runs"]) == 4


def test_workload_handoff_cold_policy_requires_binding_and_new_process(
    tmp_path: Path, scenario: Scenario, monkeypatch: pytest.MonkeyPatch
) -> None:
    import inferyard.runtime.batch_state as state

    config = scenario[0].config.config.to_dict()
    config["execution"]["require_fresh_process"] = True
    scenario = (
        replace(
            scenario[0], config=replace(scenario[0].config, config=Document.parse("config", config))
        ),
        *scenario[1:],
    )
    request, deps = batch(tmp_path, scenario, BatchOptions(second=True))
    code, first = asyncio.run(execute_async(request, deps))
    assert code == 3 and first.status == "awaiting_handoff" and len(scenario[2]) == 8
    code, waiting = asyncio.run(execute_async(request, deps))
    assert code == 3 and waiting.status == "awaiting_handoff" and len(scenario[2]) == 8
    old = scenario[0].config.config.to_dict()["endpoint"]
    same = replace(
        request,
        workload_id="w2",
        server_pid=old["server_pid"],
        endpoint_url=old["url"],
        handoff_note="synthetic operator handoff",
    )
    monkeypatch.setattr(state, "process_start_ticks", lambda _: old["process_start_ticks"])
    with pytest.raises(PreflightError, match="new_service_process"):
        asyncio.run(execute_async(same, deps))
    new_pid = old["server_pid"] + 100_000

    def ticks(pid: int) -> int:
        if pid == old["server_pid"]:
            raise PreflightError("service_process_unavailable")
        assert pid == new_pid
        return 555

    monkeypatch.setattr(state, "process_start_ticks", ticks)
    code, final = asyncio.run(execute_async(replace(same, server_pid=new_pid), deps))
    assert code == 0 and len(scenario[2]) == 16
    binding = TrialEvidence(Path(final.details["runs"][-1])).observation("service_binding")
    assert binding["effective_endpoint"]["server_pid"] == new_pid
