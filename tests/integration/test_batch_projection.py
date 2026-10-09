"""Real batch/resume orchestration consumes the same fields through a light read."""

import asyncio
import json
import shutil
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.application.verification import execute as verify
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json
from inferyard.reporting.repetition_report import (
    verify_repetition_summary,
    write_repetition_summary,
)
from inferyard.reporting.report import write_report
from inferyard.runtime import batch_state
from inferyard.runtime.batch_runner import _next, _result, execute_async
from tests.integration.test_batch_runner import batch
from tests.integration.test_batch_runner import scenario as batch_scenario
from tests.unit.test_run_projection import assert_projection, reseal_file

scenario = batch_scenario


async def cancel_batch(request, deps, calls):
    task = asyncio.create_task(execute_async(request, deps))
    while len(calls) < 6 and not task.done():
        await asyncio.sleep(0.001)
    task.cancel()
    return await task


@pytest.mark.parametrize("mode", ["repeats", "resume", "capacity", "duration", "duration_limit"])
def test_history_all_consumers_match_full_reduction(tmp_path, scenario, mode):
    request, deps = batch(
        tmp_path,
        scenario,
        capacity=mode == "capacity",
        duration_limit=20 if mode == "duration" else 1 if mode == "duration_limit" else None,
    )
    if mode == "resume":
        code, result = asyncio.run(cancel_batch(request, deps, scenario[2]))
        assert code == 130
        code, result = asyncio.run(
            execute_async(CommandRequest("resume", from_run=Path(result.details["runs"][0])), deps)
        )
        assert code == 0
        code, result = asyncio.run(execute_async(request, deps))
        assert code == 0 and len(result.details["runs"]) == 4
    else:
        if mode == "capacity":
            scenario[3]["fail_index"] = 6
        code, result = asyncio.run(execute_async(request, deps))
        assert code == (3 if mode in ("capacity", "duration_limit") else 0)
    root = request.output_root
    plan, loaded = read_frozen_plan(root / "frozen-plan/plan.json")
    full = batch_state.history(root, plan, loaded, allow_tool_change=True, full_verification=True)
    projected = batch_state.history(root, plan, loaded, allow_tool_change=True)
    for actual, expected in zip(projected, full, strict=True):
        assert actual["path"] == expected["path"]
        assert_projection(actual["path"])
    assert batch_state.remaining_budget(plan, full) == batch_state.remaining_budget(plan, projected)
    full_next, full_parent = _next(plan, full)
    light_next, light_parent = _next(plan, projected)
    assert full_next == light_next
    assert (full_parent or {}).get("run") == (light_parent or {}).get("run")
    assert _result(request, root, plan, full, "fixture") == _result(
        request, root, plan, projected, "fixture"
    )
    if mode == "capacity":
        assert asyncio.run(execute_async(request, deps))[1].status == "capacity_scan_stopped"
    if mode == "duration_limit":
        assert asyncio.run(execute_async(request, deps))[1].status == "duration_requires_new_window"


def test_runtime_projects_but_offline_batch_and_reports_use_full_reader(
    tmp_path, scenario, monkeypatch
):
    request, deps = batch(tmp_path, scenario)
    _, result = asyncio.run(execute_async(request, deps))
    calls = len(scenario[2])
    with monkeypatch.context() as patch:
        patch.setattr(batch_state, "read_trial", lambda *a, **k: pytest.fail("full runtime replay"))
        assert asyncio.run(execute_async(request, deps))[0] == 0
    with monkeypatch.context() as patch:
        patch.setattr(
            batch_state, "read_run_projection", lambda *a: pytest.fail("projection in verifier")
        )
        assert verify(CommandRequest("verify", run=request.output_root))[0] == 0
        out = tmp_path / "repeat-report"
        write_repetition_summary(request.output_root, out)
        assert verify_repetition_summary(out)["verified"]
        write_report([Path(p) for p in result.details["runs"]], tmp_path / "report")
    assert len(scenario[2]) == calls


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
        ("budget_seal", "budget_evidence_not_sealed"),
    ],
)
def test_batch_rejections_remain(tmp_path, scenario, change, reason):
    request, deps = batch(tmp_path, scenario)
    _, result = asyncio.run(execute_async(request, deps))
    root = request.output_root
    run_root = Path(result.details["runs"][0])
    if change in ("diagnostic", "tool"):
        data = read_json(run_root / "run.json")
        if change == "diagnostic":
            data["diagnostic"] = not data["diagnostic"]
        else:
            # Removing the optional scoped identity leaves a structurally valid run.
            data.pop("implementation_identity")
        (run_root / "run.json").write_bytes(json_bytes(data))
        reseal_file(run_root, "run.json")
    elif change == "budget":
        data = read_json(run_root / "execution-budget.json")
        data["elapsed_seconds"] = -1
        (run_root / "execution-budget.json").write_bytes(json_bytes(data))
        reseal_file(run_root, "execution-budget.json")
    else:
        data = read_json(run_root / "manifest.json")
        del data["files"]["execution-budget.json"]
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
        for name in ("run.json", "selection.json", "events.jsonl", "memory.jsonl"):
            path = target / name
            values = [json.loads(line) for line in path.read_bytes().splitlines()]
            for value in values:
                value["run_id"] = "cloned-child"
            path.write_bytes(b"".join(json_bytes(v) for v in values))
            reseal_file(target, name)
        manifest = read_json(target / "manifest.json")
        manifest["run_id"] = "cloned-child"
        (target / "manifest.json").write_bytes(json_bytes(manifest))
    elif change == "disconnected":
        data = read_json(target / "run.json")
        data["parent_run_id"] = "missing-parent"
        (target / "run.json").write_bytes(json_bytes(data))
        reseal_file(target, "run.json")
    plan, loaded = read_frozen_plan(request.output_root / "frozen-plan/plan.json")
    for full in (False, True):
        with pytest.raises(EvidenceError, match=reason):
            batch_state.history(
                request.output_root, plan, loaded, allow_tool_change=True, full_verification=full
            )


def test_unscorable_scores_preserve_result_exit_code(tmp_path, scenario):
    from inferyard.analysis.scoring import unscorable

    request, deps = batch(tmp_path, scenario)
    deps.scorer = lambda case, *_: unscorable(case["category"], "fixture_scoring_failure")
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 3 and len(result.details["runs"]) == 3
    for path in result.details["runs"]:
        data = assert_projection(Path(path))
        assert all(r["score"]["quality_state"] == "unscorable" for r in data["requests"])
    code, again = asyncio.run(execute_async(request, deps))
    assert code == 3
    assert again.details == result.details
