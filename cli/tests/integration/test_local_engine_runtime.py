"""Controlled transport through the production formal runner and offline reader."""

import asyncio
from copy import deepcopy
from dataclasses import replace
from pathlib import Path

import pytest

import inferyard.runtime.lock as locking
from inferyard.cli import main
from inferyard.contracts.validation import Document
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import read_json
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.runtime.runner import execute_async
from inferyard.runtime.trial_runner import TrialDependencies
from tests.integration.test_runner import scenario  # noqa: F401
from tests.local_engine_runtime.identity import freeze
from tests.local_engine_transport.conftest import rig  # noqa: F401
from tests.unit.test_local_engine_contract import local_config  # noqa: F401


class ControlledSafety:
    def __init__(self, policy, config, environment):
        self.policy, self.last = policy, None

    def metadata(self):
        return {"policy": self.policy, "sensors": [], "scope": "controlled_test"}

    def check(self, *, periodic=False):
        self.last = {
            "temperature_samples": [],
            "environment": None,
            "external_cpu_percent": None,
        }


@pytest.fixture
def local_run(scenario, local_config, rig, tmp_path):  # noqa: F811
    command, old_deps, _, _ = scenario
    config = deepcopy(local_config)
    original = command.config.config.to_dict()
    for key in ("endpoint", "output", "telemetry", "execution"):
        config[key] = original[key]
    config["engine"].update(family="vllm", served_model="fixture")
    config["model"]["local_path"] = "/fixture/model"
    state = rig("vllm")
    config["endpoint"]["url"] = state.binding["origin"]
    state.binding.update(
        pid=config["endpoint"]["server_pid"],
        start_ticks=config["endpoint"]["process_start_ticks"],
    )
    config["execution"]["warmup_count"] = 0
    state.adapter._verify_binding = lambda _: deepcopy(state.binding)
    old_preflight = old_deps.preflight
    local, files = freeze(config, state, tmp_path)

    def preflight(value):
        identity, _ = old_preflight(value)
        from inferyard.platforms.identity import hash_file

        frozen = deepcopy(local)
        frozen["binding"] = read_json(Path(value["endpoint"]["binding_path"]))
        binding_file = hash_file(
            Path(value["endpoint"]["binding_path"]), value["endpoint"]["binding_sha256"]
        )
        identity.update(origin=state.binding["origin"], local_engine=frozen)
        return identity, [
            *(item for item in files if item.path != config["endpoint"]["binding_path"]),
            binding_file,
        ]

    deps = TrialDependencies(
        preflight=preflight,
        guard=old_deps.guard,
        sampler=old_deps.sampler,
        adapter=lambda origin, secret: state.adapter,
        safety=ControlledSafety,
    )
    command = replace(
        command, config=replace(command.config, config=Document.parse("config", config))
    )
    return command, deps, state


def test_formal_local_run_seals_and_renders_offline(local_run, tmp_path, capsys):
    command, deps, state = local_run
    code, result = asyncio.run(execute_async(command, deps))
    root = Path(result.evidence_dir)
    data = read_trial(root)
    assert code == 0, result.limitations
    assert data["summary"]["counts"]["completed"] == len(command.config.bundle.to_dict()["cases"])
    assert len(state.posts) == 2 + data["summary"]["counts"]["planned"]
    assert read_json(locking.STATE_PATH)["dirty"] is False
    assert TrialEvidence(root).observation("safety_policy")["policy"]["require_temperature"] is True
    assert data["summary"]["engine_observation"]["engine_internal_drain"]["value"] is None
    assert [body["stream"] for body in state.posts[:2]] == [False, True]
    before = len(state.posts)
    import shutil

    shutil.rmtree(tmp_path / "synthetic-runtime")
    shutil.rmtree(tmp_path / "synthetic-model")
    for name in ("runtime.json", "assets.json", "binding.json"):
        (tmp_path / name).unlink()
    report = tmp_path / "report"
    assert main(["report", "--runs", str(root), "--out", str(report)]) == 0
    capsys.readouterr()
    html = (report / "report.html").read_text()
    assert "None 线程" not in html
    assert "model asset manifest sha-256" in html
    assert "runtime snapshot sha-256" in html
    assert main(["verify", "--path", str(report)]) == 0
    capsys.readouterr()
    assert len(state.posts) == before


def test_local_missing_queue_stops_before_any_generation(local_run):
    command, deps, state = local_run
    state.metrics = "vllm:num_requests_running 0\n"
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 2, result.limitations
    assert state.posts == []
    assert read_json(locking.STATE_PATH)["dirty"] is True


def intercept(state, fault):
    """Fault the HTTP boundary, retaining production adapter parsing and lifecycle."""
    import httpx

    original = state.adapter.client._transport.handle_async_request

    async def handler(request):
        response = await original(request)
        if len(state.posts) >= 3:
            return await fault(request, response)
        return response

    state.adapter.client._transport = httpx.MockTransport(handler)


def assert_stopped(result, *, terminal):
    data = read_trial(Path(result.evidence_dir))
    assert [row["execution_state"] for row in data["requests"]] == [
        terminal,
        "not_executed",
        "not_executed",
    ]
    counts = data["summary"]["counts"]
    assert counts["planned"] == 3
    assert (
        sum(counts[key] for key in ("completed", "failed", "cancelled", "invalid", "not_executed"))
        == 3
    )
    assert data["summary"]["completeness"] == "incomplete"
    assert read_json(locking.STATE_PATH)["dirty"] is True
    return data


def test_completed_http_then_queue_failure_retains_incomplete_original(local_run):
    import httpx

    command, deps, state = local_run

    async def fault(request, response):
        if request.url.path == "/metrics":
            return httpx.Response(503, text="observer unavailable")
        return response

    intercept(state, fault)
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 3
    assert len(state.posts) == 3
    assert_stopped(result, terminal="completed")


@pytest.mark.parametrize("fault_kind", ["truncated", "cancelled", "safety"])
def test_uncertain_generation_stays_dirty_even_with_zero_queue(local_run, fault_kind):
    import httpx

    from inferyard.evidence.storage import read_jsonl
    from inferyard.runtime.safety import SafetyGuard

    command, deps, state = local_run
    started = asyncio.Event()

    class Stream(httpx.AsyncByteStream):
        async def __aiter__(self):
            started.set()
            yield b'data: {"choices":[{"index":0,"delta":{"content":"partial"}}]}\n\n'
            if fault_kind != "truncated":
                await asyncio.sleep(10)

    async def fault(request, response):
        if request.method == "POST":
            return httpx.Response(
                200, stream=Stream(), headers={"content-type": "text/event-stream"}
            )
        return response

    class Sensors:
        sources = [{"metric_name": "temperature"}]

        def collect(self, *_):
            return [{"value": 85 if started.is_set() else 40}]

        def metadata(self):
            return []

    if fault_kind == "safety":
        deps.safety = lambda policy, config, environment: SafetyGuard(
            policy, config, lambda: config["conditions"], sensors=Sensors()
        )
    intercept(state, fault)

    async def run():
        task = asyncio.create_task(execute_async(command, deps))
        if fault_kind == "cancelled":
            await asyncio.wait_for(started.wait(), 5)
            task.cancel()
        return await task

    code, result = asyncio.run(run())
    assert code == (130 if fault_kind == "cancelled" else 3)
    assert len(state.posts) == 3
    assert_stopped(result, terminal="failed" if fault_kind == "truncated" else "cancelled")
    assert "num_requests_running 0" in state.metrics
    events, issues = read_jsonl(Path(result.evidence_dir) / "events.jsonl")
    assert not issues
    assert not any(row["event_type"] == "idle_observed" for row in events)
    assert any(row["event_type"] == "content" for row in events)
    if fault_kind == "safety":
        trace, issues = read_jsonl(Path(result.evidence_dir) / "safety-checks.jsonl")
        assert not issues
        errors = [row for row in trace if row["outcome"] == "error"]
        assert len(errors) == 1
        assert errors[0]["trigger"] == "periodic"
        assert errors[0]["observation"]["temperature_samples"] == [{"value": 85}]
        assert errors[0]["error"] == {
            "type": "PreflightError",
            "reason": "temperature_safety_threshold_reached",
        }


def test_sealed_queue_tampering_rejected_before_report(local_run, tmp_path, capsys):
    import json

    from inferyard.evidence.storage import EvidenceError, read_jsonl

    command, deps, _ = local_run
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 0
    root = Path(result.evidence_dir)
    (root / "manifest.json").unlink()
    events, _ = read_jsonl(root / "events.jsonl")
    observed = next(row for row in events if row["event_type"] == "native_observed")
    observed["data"]["signals"]["queue_snapshot"]["observation"]["idle"] = False
    (root / "events.jsonl").write_text("".join(json.dumps(row) + "\n" for row in events))
    with pytest.raises(EvidenceError, match="local_engine_queue"):
        read_trial(root)
    assert main(["report", "--runs", str(root), "--out", str(tmp_path / "tampered-report")]) == 4
    capsys.readouterr()
    assert not (tmp_path / "tampered-report" / "index.html").exists()


def test_second_scope_requires_explicit_handoff_and_new_process(
    local_run,
    tmp_path,
    monkeypatch,
    rig,  # noqa: F811
):
    from inferyard.platforms.identity import PreflightError
    from inferyard.runtime import batch_state
    from inferyard.runtime.batch_runner import execute_async as execute_batch
    from tests.integration.test_batch_runner import batch

    command, deps, state = local_run
    from tests.integration import test_batch_runner

    original_experiment = test_batch_runner.experiment

    def experiment():
        value = original_experiment()
        value["safety"] = {
            **command.config.config.to_dict()["safety"],
            "max_external_cpu_percent": None,
        }
        return value

    from inferyard.evidence.storage import json_bytes
    from inferyard.platforms.identity import hash_file

    config = command.config.config.to_dict()
    worker_pid = config["endpoint"]["server_pid"] + 200000
    state.binding["workers"] = [
        {
            "pid": worker_pid,
            "parent_pid": config["endpoint"]["server_pid"],
            "start_ticks": 777,
            **{
                key: state.binding[key]
                for key in ("executable_sha256", "argv_sha256", "cwd_sha256")
            },
        }
    ]
    original_binding = Path(config["endpoint"]["binding_path"])
    frozen = read_json(original_binding)
    frozen["service"] = deepcopy(state.binding)
    original_binding.write_bytes(json_bytes(frozen))
    config["endpoint"]["binding_sha256"] = hash_file(original_binding).sha256
    command = replace(
        command, config=replace(command.config, config=Document.parse("config", config))
    )
    monkeypatch.setattr(test_batch_runner, "experiment", experiment)
    request, deps = batch(tmp_path, (command, deps, state.posts, {}), second=True)
    code, first = asyncio.run(execute_batch(request, deps))
    assert code == 3 and first.status == "awaiting_handoff", first
    assert len(state.posts) == 5
    runs = first.details["runs"]
    assert len(runs) == 1
    assert read_trial(Path(runs[0]))["summary"]["counts"]["completed"] == 3
    code, waiting = asyncio.run(execute_batch(request, deps))
    assert code == 3 and waiting.status == "awaiting_handoff"
    endpoint = command.config.config.to_dict()["endpoint"]
    same = replace(
        request,
        workload_id="w2",
        server_pid=endpoint["server_pid"],
        endpoint_url=endpoint["url"],
        handoff_note="controlled same-process handoff",
        service_binding_path=Path(endpoint["binding_path"]),
    )
    monkeypatch.setattr(
        batch_state, "process_start_ticks", lambda _: endpoint["process_start_ticks"]
    )
    with pytest.raises(PreflightError, match="new_service_process|drain_evidence_missing"):
        asyncio.run(execute_batch(same, deps))
    assert len(state.posts) == 5
    assert len(list((tmp_path / "batch" / "runs").iterdir())) == 1

    from inferyard.evidence.storage import json_bytes

    replacement = rig("vllm")
    replacement.binding = deepcopy(state.binding)
    replacement.binding.update(pid=endpoint["server_pid"] + 100000, start_ticks=555, workers=[])
    replacement.adapter._verify_binding = lambda _: deepcopy(replacement.binding)
    deps.adapter = lambda origin, secret: replacement.adapter
    artifact = read_json(Path(endpoint["binding_path"]))
    artifact["service"] = deepcopy(replacement.binding)
    binding_path = tmp_path / "replacement-binding.json"
    binding_path.write_bytes(json_bytes(artifact))

    worker_alive = True

    def ticks(pid):
        if pid == worker_pid:
            if worker_alive:
                return 777
            raise PreflightError("service_process_unavailable")
        if pid == endpoint["server_pid"]:
            raise PreflightError("service_process_unavailable")
        assert pid == replacement.binding["pid"]
        return 555

    monkeypatch.setattr(batch_state, "process_start_ticks", ticks)
    monkeypatch.setattr(locking, "process_start_ticks", ticks)
    new = replace(same, server_pid=replacement.binding["pid"], service_binding_path=binding_path)
    with pytest.raises(PreflightError, match="binding"):
        asyncio.run(execute_batch(replace(new, service_binding_path=None), deps))
    assert replacement.posts == []
    with pytest.raises(PreflightError, match="worker.*alive"):
        asyncio.run(execute_batch(new, deps))
    assert replacement.posts == []
    worker_alive = False
    code, final = asyncio.run(execute_batch(new, deps))
    assert code == 0 and final.status == "finished", final
    assert len(state.posts) == len(replacement.posts) == 5
    assert len(final.details["runs"]) == 2
    assert [
        read_trial(Path(path))["summary"]["counts"]["completed"] for path in final.details["runs"]
    ] == [3, 3]
    report = tmp_path / "combined-report"
    assert main(["report", "--runs", *final.details["runs"], "--out", str(report)]) == 0
    assert main(["verify", "--path", str(report)]) == 0
    from inferyard.analysis.comparison import compare_trials

    comparison = compare_trials(*(read_trial(Path(path)) for path in final.details["runs"]))
    assert comparison["eligibility"]["performance"] is False
    assert "local_engine_controlled_comparison_not_supported" in comparison["blockers"]
    assert any(row["field"] == "config.model.content_sha256" for row in comparison["conditions"])
