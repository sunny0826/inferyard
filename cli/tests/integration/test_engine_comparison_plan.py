"""One local synthetic plan executes all 12 slots and reads back as v2 slots."""

import asyncio
import hashlib
import json
import shutil
from collections.abc import Awaitable, Callable
from pathlib import Path
from types import SimpleNamespace

import httpx
import pytest

from inferyard.analysis.comparison_types import JSONRecord
from inferyard.application.types import CommandRequest, CommandResult
from inferyard.errors import EvidenceError
from inferyard.evidence.artifact_seal import seal
from inferyard.contracts.validation import json_bytes
from inferyard.runtime import batch_state
from inferyard.application.execution import execute_async
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


def _derive(plan_path: Path, batch_path: Path, out: Path) -> tuple[int, CommandResult]:
    from inferyard.application.dispatch import default_backend

    return default_backend(
        CommandRequest(
            "compare-engines", frozen_plan=plan_path, batch=batch_path, out=out
        )
    )


def _mutate_binding(run_dir: Path) -> None:
    path = run_dir / "observations.jsonl"
    rows = [json.loads(line) for line in path.read_text().splitlines()]
    for row in rows:
        if row["kind"] == "service_binding":
            row["payload"]["previous_run_id"] = "missing-previous"
            row["payload_sha256"] = hashlib.sha256(json_bytes(row["payload"])).hexdigest()
            break
    path.write_bytes(b"".join(json_bytes(row) for row in rows))
    assertions._rehash_manifest(run_dir, "observations.jsonl")


def _assert_seal_guards(tmp_path: Path, output: Path) -> None:
    from inferyard.application.verification import execute as verify

    result_path = output / "engine-comparison.json"
    tampered = tmp_path / "v2-tampered"
    shutil.copytree(output, tampered)
    (tampered / "engine-comparison.json").write_bytes(result_path.read_bytes() + b" ")
    with pytest.raises(EvidenceError, match="presentation_bytes_changed"):
        verify(CommandRequest("verify", run=tampered))

    resealed = tmp_path / "v2-resealed"
    shutil.copytree(output, resealed)
    saved = json.loads((resealed / "engine-comparison.json").read_text())
    saved["eligible"] = not saved["eligible"]
    (resealed / "engine-comparison.json").write_bytes(json_bytes(saved))
    (resealed / "artifact-manifest.json").unlink()
    seal(resealed, ("engine-comparison.json", "report.html"))
    with pytest.raises(EvidenceError, match="engine_comparison_recomputation_mismatch"):
        verify(CommandRequest("verify", run=resealed))

    html_changed = tmp_path / "v2-html-changed"
    shutil.copytree(output, html_changed)
    html = (html_changed / "report.html").read_text()
    (html_changed / "report.html").write_text(html.replace("v2", "v2 changed", 1))
    (html_changed / "artifact-manifest.json").unlink()
    seal(html_changed, ("engine-comparison.json", "report.html"))
    assert verify(CommandRequest("verify", run=html_changed))[0] == 0
    with pytest.raises(EvidenceError, match="engine_comparison_html_recomputation_mismatch"):
        verify(CommandRequest("verify", run=html_changed, rerender=True))


def _assert_blocked_guards(
    tmp_path: Path, plan: JSONRecord, plan_path: Path, batch_path: Path
) -> None:
    from inferyard.application.verification import execute as verify

    target_trial = plan["trials"][-1]["trial_id"]
    wrong_batch = tmp_path / "v2-wrong-plan-batch"
    shutil.copytree(batch_path, wrong_batch)
    assertions.mutate_run_documents(
        assertions.run_paths(wrong_batch)[target_trial],
        lambda run, _: run.__setitem__("plan_sha256", "f" * 64),
    )
    code, blocked = _derive(plan_path, wrong_batch, tmp_path / "v2-wrong-plan")
    assert code == 0 and blocked.details["eligible"] is False
    assert blocked.details["empty_metrics"]
    assert verify(CommandRequest("verify", run=tmp_path / "v2-wrong-plan"))[0] == 0

    unplanned_batch = tmp_path / "v2-unplanned-batch"
    shutil.copytree(batch_path, unplanned_batch)
    assertions.mutate_run_documents(
        assertions.run_paths(unplanned_batch)[target_trial],
        lambda run, selection: (
            run.__setitem__("trial_id", "trial-unplanned"),
            selection.__setitem__("trial_id", "trial-unplanned"),
        ),
    )
    code, unplanned = _derive(plan_path, unplanned_batch, tmp_path / "v2-unplanned")
    assert code == 0 and unplanned.details["eligible"] is False
    assert unplanned.details["run_set"]["unplanned"]

    broken_batch = tmp_path / "v2-broken-batch"
    shutil.copytree(batch_path, broken_batch)
    _mutate_binding(assertions.run_paths(broken_batch)[target_trial])
    code, broken = _derive(plan_path, broken_batch, tmp_path / "v2-broken")
    assert code == 0 and broken.details["eligible"] is False
    assert any(
        row["reason"] == "comparison_handoff_broken"
        for row in broken.details["diagnostics"]
    )


def _assert_moved_sources(tmp_path: Path, plan_path: Path, batch_path: Path, output: Path) -> None:
    from inferyard.application.verification import execute as verify

    old_plan, old_batch = plan_path.parent, batch_path
    moved = tmp_path / "moved-sources"
    (moved / "plan").parent.mkdir(parents=True)
    shutil.move(old_plan, moved / "plan")
    shutil.move(old_batch, moved / "batch")
    with pytest.raises(EvidenceError, match="engine_comparison_recomputation_mismatch"):
        verify(CommandRequest("verify", run=output))
    request = CommandRequest(
        "verify",
        run=output,
        rerender=True,
        source_roots=[
            f"{old_plan}={moved / 'plan'}",
            f"{old_batch}={moved / 'batch'}",
        ],
    )
    assert verify(request)[0] == 0


def _assert_v2_report_guards(
    tmp_path: Path,
    plan: JSONRecord,
    plan_path: Path,
    batch_path: Path,
    output: Path,
) -> None:
    _assert_seal_guards(tmp_path, output)
    _assert_blocked_guards(tmp_path, plan, plan_path, batch_path)
    _assert_moved_sources(tmp_path, plan_path, batch_path, output)


async def _cached_completion(request: httpx.Request) -> httpx.Response:
    body = json.loads(request.content)
    usage = {
        "prompt_tokens": 2,
        "completion_tokens": 1,
        "total_tokens": 3,
        "prompt_tokens_details": {"cached_tokens": 0},
    }
    choice = {"index": 0, "finish_reason": "stop"}
    if body["stream"]:
        chunks = [
            {"choices": [{"index": 0, "delta": {"content": "ok"}, "finish_reason": None}]},
            {"choices": [{**choice, "delta": {}}]},
            {"choices": [], "usage": usage},
        ]
        raw = (
            "".join("data: " + json_bytes(c).decode("utf-8") + "\n\n" for c in chunks)
            + "data: [DONE]\n\n"
        )
        return httpx.Response(200, text=raw, headers={"content-type": "text/event-stream"})
    return httpx.Response(
        200,
        json={"choices": [{**choice, "message": {"content": "ok"}}], "usage": usage},
    )


def _run_twelve_slots(
    tmp_path: Path,
    batch_scenario: helpers.Scenario,
    rig: Callable[[str], SimpleNamespace],
    monkeypatch: pytest.MonkeyPatch,
    completion: Callable[[httpx.Request], Awaitable[httpx.Response]] | None = None,
) -> tuple[helpers.SlotExecution, JSONRecord]:
    _, deps, calls, _ = batch_scenario
    factory = rig if completion is None else lambda family: rig(family, completion=completion)
    plan, sides, states, identities, identity_files = helpers.local_inputs(tmp_path, factory)
    slots, bindings = helpers.slot_inputs(plan, identities, tmp_path)
    assert len(slots) == 12
    helpers.assert_family_dispatch(sides)
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
    return execution, plan


def test_twelve_slots_execute_with_handoffs_and_read_back(
    tmp_path: Path,
    batch_scenario: helpers.Scenario,
    rig: Callable[[str], SimpleNamespace],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    execution, plan = _run_twelve_slots(tmp_path, batch_scenario, rig, monkeypatch)
    from inferyard.application.verification import execute as verify

    output = tmp_path / "comparison-v2"
    code, derived = _derive(
        execution.request.frozen_plan, execution.request.output_root, output
    )
    assert code == 0 and derived.status == "derived"
    saved = json.loads((output / "engine-comparison.json").read_text())
    assert saved["definition"] == "engine-comparison.v2"
    assert saved["eligible"] == all(
        item["eligible"] for item in saved["comparisons"].values()
    )
    assert len(saved["source_runs"]) == 12
    code, verified = verify(CommandRequest("verify", run=output, rerender=True))
    assert code == 0 and verified.details["artifact_type"] == "engine-comparison"
    assert verified.details["render_checked"] is True
    _assert_v2_report_guards(
        tmp_path,
        plan,
        execution.request.frozen_plan,
        execution.request.output_root,
        output,
    )


def test_v2_positive_metrics_render_and_verify(
    tmp_path: Path,
    batch_scenario: helpers.Scenario,
    rig: Callable[[str], SimpleNamespace],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    execution, _ = _run_twelve_slots(
        tmp_path, batch_scenario, rig, monkeypatch, completion=_cached_completion
    )
    output = tmp_path / "comparison-v2-positive"
    code, derived = _derive(
        execution.request.frozen_plan, execution.request.output_root, output
    )
    assert code == 0 and derived.status == "derived"
    saved = json.loads((output / "engine-comparison.json").read_text())
    assert saved["eligible"] is True
    assert all(item["eligible"] for item in saved["comparisons"].values())
    assert saved["empty_metrics"] == []
    html = (output / "report.html").read_text()
    comparison = next(iter(saved["comparisons"].values()))
    metric = next(
        row for row in comparison["blocks"][0]["medians"].values()
        if row["difference"] is not None
    )
    medians = (
        f"{metric['left']:.2f} / {metric['right']:.2f} / "
        f"{metric['difference']:.2f}"
    )
    assert "无阻断，可判定。" in html
    assert "配对请求" in html
    assert medians in html
    assert any(
        verdict["metric_id"] in html
        for verdict in comparison["verdicts"].values()
    )
    assert "<h2>空指标原因</h2>" not in html
    from inferyard.application.verification import execute as verify

    code, verified = verify(CommandRequest("verify", run=output, rerender=True))
    assert code == 0 and verified.details["render_checked"] is True
