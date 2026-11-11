"""Real batch/resume orchestration consumes the same fields through a light read."""

import asyncio
import shutil
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.application.verification import execute as verify
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.errors import EvidenceError
from inferyard.contracts.validation import json_bytes
from inferyard.evidence.storage import read_json, read_jsonl
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.reporting.repetition_report import write_repetition_summary
from inferyard.reporting.report import write_report
from inferyard.runtime import batch_state
from inferyard.runtime.batch_runner import execute_async
from tests.integration.test_batch_runner import batch
from tests.integration.test_batch_runner import scenario as batch_scenario
from tests.layout_mutations import change_input, change_observation, digest, reseal
from tests.unit.test_run_projection import reseal_file

scenario = batch_scenario


async def cancel_batch(request, deps, calls):
    task = asyncio.create_task(execute_async(request, deps))
    while len(calls) < 6 and not task.done():
        await asyncio.sleep(0.001)
    task.cancel()
    return await task


@pytest.mark.parametrize("name", ["memory.jsonl", "environment.jsonl", "schedule.jsonl"])
@pytest.mark.parametrize("repair_seal", [False, True])
def test_skipped_timeline_validation_remains_in_verify_and_report(
    tmp_path, scenario, name, repair_seal
):
    request, deps = batch(tmp_path, scenario)
    _, result = asyncio.run(execute_async(request, deps))
    root = Path(result.details["runs"][0])
    path = root / name
    original = path.read_bytes()
    assert original
    # Same byte count isolates the intentionally deferred hash/semantic checks.
    path.write_bytes(b"{}" + b" " * (len(original) - 3) + b"\n")
    if repair_seal:
        reseal_file(root, name)
    assert asyncio.run(execute_async(request, deps))[0] == 0
    reason = (
        {
            "memory.jsonl": "invalid_artifact_version",
            "environment.jsonl": "invalid_environment_observation",
            "schedule.jsonl": "invalid_collector_schedule",
        }[name]
        if repair_seal
        else "original_evidence_hash_mismatch"
    )
    for action in (
        lambda: verify(CommandRequest("verify", run=root)),
        lambda: verify(CommandRequest("verify", run=request.output_root)),
        lambda: write_report([root], tmp_path / "bad-report"),
        lambda: write_repetition_summary(request.output_root, tmp_path / "bad-repeat"),
    ):
        with pytest.raises(EvidenceError, match=reason):
            action()


@pytest.mark.parametrize(
    "change,reason",
    [
        ("diagnostic", "batch_diagnostic_mode_changed"),
        ("tool", "batch_run_tool_changed"),
        ("budget", "invalid_execution_budget_evidence"),
        ("observations_seal", "incomplete_layout_manifest"),
    ],
)
def test_batch_rejections_remain(tmp_path, scenario, change, reason):
    request, deps = batch(tmp_path, scenario)
    _, result = asyncio.run(execute_async(request, deps))
    root = request.output_root
    run_root = Path(result.details["runs"][0])
    if change in ("diagnostic", "tool"):

        def mutate(data):
            if change == "diagnostic":
                data["diagnostic"] = not data["diagnostic"]
            else:
                data.pop("implementation_identity")

        change_input(run_root, "run", mutate, sealed=True)
    elif change == "budget":
        change_observation(
            run_root, "execution_budget", lambda data: data.update(elapsed_seconds=-1), sealed=True
        )
    else:
        data = read_json(run_root / "manifest.json")
        del data["files"]["observations.jsonl"]
        (run_root / "manifest.json").write_bytes(json_bytes(data))
    plan, loaded = read_frozen_plan(root / "frozen-plan/plan.json")
    for full in (False, True):
        with pytest.raises(EvidenceError, match=reason):
            runs = batch_state.history(
                root, plan, loaded, allow_tool_change=True, full_verification=full
            )
            batch_state.remaining_budget(plan, runs)


@pytest.mark.parametrize(
    "change,reason",
    [
        ("duplicate", "batch_run_identity_mismatch"),
        ("branch", "branched_resume_history"),
        ("disconnected", "disconnected_resume_history"),
    ],
)
def test_resume_graph_rejections_match_full_history(tmp_path, scenario, change, reason):
    request, deps = batch(tmp_path, scenario)
    _, result = asyncio.run(cancel_batch(request, deps, scenario[2]))
    parent = Path(result.details["runs"][0])
    code, result = asyncio.run(execute_async(CommandRequest("resume", from_run=parent), deps))
    assert code == 0
    child = Path(result.details["runs"][-1])
    if change in ("duplicate", "branch"):
        target = child.parent / "cloned-child"
        shutil.copytree(child, target)
    else:
        target = child
    if change == "branch":
        original_id = TrialEvidence(target).input("run")["run_id"]
        envelope = read_json(target / "inputs.json")
        envelope["run_id"] = "cloned-child"
        for kind in ("run", "selection"):
            member = envelope["documents"][kind]
            member["payload"]["run_id"] = "cloned-child"
            member["sha256"] = digest(member["payload"])
        (target / "inputs.json").write_bytes(json_bytes(envelope))
        for name in ("observations.jsonl", "request-bodies.jsonl", "events.jsonl", "memory.jsonl"):
            values, tails = read_jsonl(target / name)
            assert not tails
            for value in values:
                value["run_id"] = "cloned-child"
                if value.get("request_id"):
                    value["request_id"] = value["request_id"].replace(
                        original_id, "cloned-child", 1
                    )
            (target / name).write_bytes(b"".join(json_bytes(value) for value in values))
        manifest = read_json(target / "manifest.json")
        manifest["run_id"] = "cloned-child"
        (target / "manifest.json").write_bytes(json_bytes(manifest))
        reseal(
            target,
            "inputs.json",
            "observations.jsonl",
            "request-bodies.jsonl",
            "events.jsonl",
            "memory.jsonl",
        )
    elif change == "disconnected":
        change_input(
            target, "run", lambda data: data.update(parent_run_id="missing-parent"), sealed=True
        )
    plan, loaded = read_frozen_plan(request.output_root / "frozen-plan/plan.json")
    for full in (False, True):
        with pytest.raises(EvidenceError, match=reason):
            batch_state.history(
                request.output_root, plan, loaded, allow_tool_change=True, full_verification=full
            )
