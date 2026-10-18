"""Progress selects dedicated commands, including batches without any trial."""

import json
import shutil

from tests.integration.context_speed_fixture import scenario, tree_hashes  # noqa: F401
from tests.integration.context_speed_fixture import speed_service as service_fixture
from tests.integration.test_context_speed_cli import invoke, run_speed

speed_service = service_fixture


def test_progress_with_zero_and_complete_batches(speed_service, monkeypatch, capsys):
    batch, _ = run_speed(speed_service, monkeypatch, capsys)
    monkeypatch.setattr("inferyard.config.community_workspace.require_review", lambda _: None)
    workspace = speed_service["root"] / "workspace"
    code, result = invoke(capsys, "init", "--bundle", "zh-context-speed", "--out", workspace)
    assert code == 0, result
    copied = workspace / "results" / "speed-batch-1"
    shutil.copytree(batch, copied)
    for zero in (False, True):
        if zero:
            for run in (copied / "runs").iterdir():
                shutil.rmtree(run)
        before = tree_hashes(workspace)
        code, result = invoke(capsys, "progress", "--workspace", workspace)
        assert code == 0, result
        assert result["next_action"] == "speed_results"
        assert result["next_command"] == f"inferyard speed-results --batch {copied}"
        assert not result["default_scope_complete"]
        assert before == tree_hashes(workspace)


def test_progress_preparation_plan_and_new_batch(speed_service, monkeypatch, capsys):
    monkeypatch.setattr("inferyard.config.community_workspace.require_review", lambda _: None)
    workspace = speed_service["root"] / "workspace"
    assert invoke(capsys, "init", "--bundle", "zh-context-speed", "--out", workspace)[0] == 0
    preflight = workspace / "preflight/device-preflight.json"
    preflight.write_text(
        json.dumps(
            {
                "kind": "device_preflight.v1",
                "hardware": {"platform": "Darwin"},
                "recommendation": {"status": "recommended"},
            }
        )
    )
    code, result = invoke(capsys, "progress", "--workspace", workspace)
    assert code == 0
    assert "--profile context-speed --context-size" in result["next_command"]
    source = speed_service["write"]()
    candidate = workspace / "candidates/one"
    candidate.mkdir()
    (candidate / "preparation.json").write_text(
        json.dumps(
            dict(
                kind="config_candidate.v1",
                schema_version=3,
                candidate=str(source),
                bundle=str(workspace / "bundles/zh-context-speed.json"),
            )
        )
    )
    bound = workspace / "bound/one"
    bound.mkdir()
    (bound / "preparation.json").write_text(
        json.dumps(
            dict(
                kind="config_binding.v1",
                schema_version=3,
                candidate=str(source),
                config=str(source),
            )
        )
    )
    code, result = invoke(capsys, "progress", "--workspace", workspace)
    assert code == 0
    assert result["next_action"] == "freeze_plan"
    from tests.integration.context_speed_fixture import allow_synthetic_review

    allow_synthetic_review(monkeypatch)
    plan = workspace / "results/speed-plan-1"
    assert (
        invoke(capsys, "plan", "--preset", "context-speed", "--config", source, "--out", plan)[0]
        == 0
    )
    code, result = invoke(capsys, "progress", "--workspace", workspace)
    assert code == 0
    assert result["next_action"] == "run"
    assert str(plan / "plan.json") in result["next_command"]
    assert str(workspace / "results/speed-batch-1") in result["next_command"]
