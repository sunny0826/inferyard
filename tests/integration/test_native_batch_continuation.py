"""Native serial repeats/resumes keep client completion distinct from engine drain."""

import asyncio
import json
from pathlib import Path

import pytest

import inferyard.runtime.lock as locking
from inferyard.analysis.scoring import score_case
from inferyard.application.types import CommandRequest
from inferyard.config.loader import load_config
from inferyard.config.planning import write_plan
from inferyard.config.single_plan import compile_single_plan
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, read_json, sha256_file
from inferyard.runtime.batch_runner import execute_async
from inferyard.runtime.trial_runner import TrialDependencies
from tests.integration.test_lab_cli import lab_scenario, write_config  # noqa: F401
from tests.integration.test_native_cli import native_scenario  # noqa: F401
from tests.integration.test_runner import scenario  # noqa: F401


@pytest.fixture
def native_batch(native_scenario, tmp_path, monkeypatch):  # noqa: F811
    command, deps, service = native_scenario
    config_path = tmp_path / "native-plan.toml"
    write_config(command, config_path)
    loaded = load_config(config_path)
    exp = compile_single_plan(loaded.config.to_dict(), loaded.bundle.to_dict())["experiment"]
    exp["workloads"][0]["repeats"] = 2
    exp["budget"]["max_requests"] *= 2
    exp["budget"]["max_wall_seconds"] *= 2
    for field, path in (("config", config_path), ("bundle", tmp_path / "bundle.json")):
        exp["workloads"][0][field] = {"path": path.name, "sha256": sha256_file(path)}
    source = tmp_path / "experiment.json"
    source.write_text(json.dumps(exp))
    frozen = tmp_path / "frozen"
    write_plan(source, frozen)
    trial_deps = TrialDependencies(
        preflight=deps.preflight, adapter=deps.adapter, guard=deps.guard, sampler=deps.sampler
    )
    monkeypatch.setattr("inferyard.runtime.trial_runner.require_review", lambda _: None)
    request = CommandRequest(
        "run", frozen_plan=frozen / "plan.json", output_root=tmp_path / "batch"
    )
    return request, trial_deps, service


def assert_client_only(root):
    metadata = {}
    data = read_trial(root, metadata=metadata)
    assert metadata["service_drain"] is None
    assert data["summary"]["engine_observation"]["engine_internal_drain"]["value"] is None
    return data


def test_native_two_repeats_keep_client_http_scope(native_batch):
    request, deps, service = native_batch
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0
    roots = [Path(p) for p in result.details["runs"]]
    assert len(roots) == 2
    for root in roots:
        data = assert_client_only(root)
        assert data["summary"]["counts"]["completed"] == 3
    proof = read_json(roots[1] / "service-reuse.json")
    assert proof["serial_continuation"] is True
    assert proof["previous_drain"] is None
    assert read_json(locking.STATE_PATH)["completion_scope"] == "client_http"
    assert read_json(locking.STATE_PATH)["dirty"] is False
    assert len(service.requests) == 16
    code, _ = asyncio.run(execute_async(request, deps))
    assert code == 0 and len(service.requests) == 16


@pytest.mark.parametrize("dirty", [False, True])
def test_native_resume_after_clean_scoring_stop_and_dirty_rejection(native_batch, dirty):
    request, deps, service = native_batch

    def fail_score(*_):
        raise EvidenceError("evidence_io_error")

    deps.scorer = fail_score
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 4 and len(service.requests) == 6
    parent = Path(result.details["runs"][0])
    data = assert_client_only(parent)
    assert data["summary"]["counts"]["not_executed"] == 2
    if dirty:
        with deps.lock() as lock:
            lock.dirty(data["run"]["run_id"], data["config"]["endpoint"], "uncertain-request")
    deps.scorer = score_case
    code, result = asyncio.run(execute_async(CommandRequest("resume", from_run=parent), deps))
    if dirty:
        assert code == 2
        assert result.details["last_stop_reason"] == "native_dirty_requires_bound_recovery"
        assert len(service.requests) == 6
        assert read_json(locking.STATE_PATH)["dirty"] is True
    else:
        assert code == 0
        resumed = assert_client_only(Path(result.details["runs"][-1]))
        assert resumed["run"]["relation"] == "resume"
        assert resumed["selection"]["case_ids"] == data["selection"]["case_ids"][1:]
        assert len(service.requests) == 13
        # The next planned repetition crosses a command boundary, with no engine drain proof.
        code, result = asyncio.run(execute_async(request, deps))
        assert code == 0 and len(result.details["runs"]) == 3
        assert len(service.requests) == 21


@pytest.mark.parametrize("failure", ["truncated", "cancelled"])
def test_native_uncertain_or_cancelled_request_blocks_resume(native_batch, failure):
    request, deps, service = native_batch
    service.options.update(fail_at=6)
    if failure == "truncated":
        service.options["truncated"] = True
        code, result = asyncio.run(execute_async(request, deps))
        assert code == 3
    else:
        service.options["delay"] = 1

        async def cancel():
            task = asyncio.create_task(execute_async(request, deps))
            while len(service.requests) < 6 and not task.done():
                await asyncio.sleep(0.001)
            task.cancel()
            return await task

        code, result = asyncio.run(cancel())
        assert code == 130
    parent = Path(result.details["runs"][0])
    data = assert_client_only(parent)
    assert data["summary"]["counts"]["not_executed"] == 2
    assert data["summary"]["counts"]["cancelled" if failure == "cancelled" else "failed"] == 1
    assert read_json(locking.STATE_PATH)["dirty"] is True
    service.options.clear()
    code, result = asyncio.run(execute_async(CommandRequest("resume", from_run=parent), deps))
    assert code == 2
    assert result.details["last_stop_reason"] == "native_dirty_requires_bound_recovery"
    assert len(service.requests) == 6
