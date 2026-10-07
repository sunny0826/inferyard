import asyncio
import hashlib
import json
import os
import sys
from copy import deepcopy
from dataclasses import replace
from pathlib import Path

import httpx
import pytest

import inferyard.runtime.lock as locking
import inferyard.runtime.runner as running
from inferyard.adapters.prism import BUILD, PrismAdapter
from inferyard.application.types import CommandRequest
from inferyard.config.bundle import content_hash, require_review
from inferyard.config.loader import load_config
from inferyard.contracts.validation import ContractError, Document
from inferyard.evidence.ledger import read_trial as rebuild
from inferyard.platforms.identity import (
    PreflightError,
    environment_snapshot,
    process_start_ticks,
)
from inferyard.runtime.runner import Dependencies, execute_async


@pytest.fixture
def scenario(tmp_path, monkeypatch, config_path, *, initialize_host=True):
    # Synthetic orchestration must not depend on the developer's free RAM.
    monkeypatch.setattr("inferyard.runtime.trial_runner.memory_available", lambda: 16 * 1024**3)
    monkeypatch.setattr(locking, "LEGACY_ROOT", None)
    monkeypatch.setattr(locking, "LOCK_PATH", tmp_path / "host.lock")
    monkeypatch.setattr(locking, "STATE_PATH", tmp_path / "host.state.json")
    if initialize_host:
        from tests.host_state_helpers import initialize

        initialize()
    loaded = load_config(config_path)
    config = loaded.config.to_dict()
    config["endpoint"].pop("api_key_env")
    config["endpoint"]["server_pid"] = os.getpid()
    # This fixture tests orchestration with a mock endpoint, not OS process identity.
    # Linux/Windows/macOS keep their native sampler; an unsupported developer host has
    # an explicit synthetic process identity rather than requiring /proc.
    if sys.platform not in ("linux", "win32", "darwin"):
        ticks = 123456
        monkeypatch.setattr(running, "process_start_ticks", lambda pid: ticks)
    else:
        ticks = process_start_ticks(os.getpid())
    config["endpoint"]["process_start_ticks"] = ticks
    config["output"]["root"] = str(tmp_path / "runs")
    config["telemetry"].update(interval_ms=10, baseline_seconds=0.015)
    config["generation"]["seed_support"] = "supported"
    config["conditions"]["cache_policy"] = "disabled"
    config["model"]["template_sha256"] = hashlib.sha256(b"fixture-template").hexdigest()
    config["engine"]["startup_args"] = [
        "-ngl",
        "0",
        "-t",
        "6",
        "-tb",
        "6",
        "--reasoning",
        "off",
        "--no-cache-prompt",
        "--no-cache-idle-slots",
        "--cache-ram",
        "0",
    ]
    libraries = tmp_path / "libraries.json"
    libraries.write_text('{"fixture.so":"' + "a" * 64 + '"}')
    config["engine"]["runtime_library_manifest"] = str(libraries)
    bundle = loaded.bundle.to_dict()
    bundle["cases"].append(dict(deepcopy(bundle["cases"][0]), case_id="instruction-02"))
    loaded = replace(
        loaded, config=Document.parse("config", config), bundle=Document.parse("bundle", bundle)
    )
    calls = []
    settings = {"fail_index": None, "busy": False}
    params = {}

    class Stream(httpx.AsyncByteStream):
        def __init__(self, content):
            self.content = content

        async def __aiter__(self):
            if isinstance(self.content, list):
                for part in self.content:
                    await asyncio.sleep(0.003)
                    yield part
                return
            await asyncio.sleep(settings.get("stream_delay", 0.03))
            yield self.content

    def handler(request):
        if request.url.path == "/slots":
            return httpx.Response(
                200,
                json=[
                    {"id": 0, "is_processing": settings["busy"], "params": params, "n_ctx": 4096}
                ],
            )
        if request.url.path == "/props":
            return httpx.Response(
                200,
                json={
                    "build_info": BUILD,
                    "total_slots": 1,
                    "default_generation_settings": {"n_ctx": 4096},
                    "model_path": config["model"]["local_path"],
                    "chat_template": "fixture-template",
                },
            )
        if request.url.path == "/apply-template":
            return httpx.Response(200, json={"prompt": "rendered"})
        if request.url.path == "/tokenize":
            return httpx.Response(200, json={"tokens": [1]})
        assert request.url.path == "/v1/chat/completions"
        body = json.loads(request.content)
        index = len(calls)
        calls.append(body)
        params.update(body)
        if index == settings["fail_index"]:
            return httpx.Response(500, json={"error": "fixture"})
        answer = '{"name":"小明","age":12}' if "姓名" in body["messages"][0]["content"] else "北京"
        answer = settings.get("answer", answer)
        usage = {"prompt_tokens": 1, "completion_tokens": settings.get("completion_tokens", 2)}
        finish = settings.get("finish_reason", "stop")
        if not body["stream"]:
            return httpx.Response(
                200,
                json={
                    "choices": [
                        {
                            "index": 0,
                            "finish_reason": finish,
                            "message": {"role": "assistant", "content": answer},
                        }
                    ],
                    "usage": usage,
                    **({"timings": settings["timings"]} if "timings" in settings else {}),
                },
            )
        event = {
            "choices": [{"index": 0, "finish_reason": finish, "delta": {"content": answer}}],
            "usage": usage,
            **({"timings": settings["timings"]} if "timings" in settings else {}),
        }
        data = ("data: " + json.dumps(event, ensure_ascii=False) + "\n\ndata: [DONE]\n\n").encode()
        if settings.get("split_content"):
            parts = [
                (
                    "data: "
                    + json.dumps({"choices": [{"index": 0, "delta": {"content": text}}]})
                    + "\n\n"
                ).encode()
                for text in answer
            ]
            event["choices"][0]["delta"] = {}
            parts.append(("data: " + json.dumps(event) + "\n\ndata: [DONE]\n\n").encode())
            return httpx.Response(200, stream=Stream(parts))
        return httpx.Response(200, stream=Stream(data))

    deps = Dependencies(
        preflight=lambda config: (
            {
                "origin": "http://127.0.0.1:8080",
                "environment": environment_snapshot(),
                "verification": "verified",
            },
            [],
        ),
        adapter=lambda origin, secret: PrismAdapter(
            origin, secret=secret, transport=httpx.MockTransport(handler)
        ),
        guard=lambda config, files: None,
    )
    # F07's small synthetic plan exercises orchestration, not formal corpus admission.
    monkeypatch.setattr(running, "require_review", lambda bundle: None)
    return CommandRequest("run", config=loaded), deps, calls, settings


def test_f07_full_pipeline_warmups_failed_case_and_no_retries(scenario):
    request, deps, calls, settings = scenario
    settings["fail_index"] = 6
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0, result
    assert len(calls) == 8  # two probes, three warmups, three formal cases
    data = rebuild(Path(result.evidence_dir))
    assert [r["execution_state"] for r in data["requests"]] == ["completed", "failed", "completed"]
    assert data["summary"]["counts"]["planned"] == 3
    assert data["summary"]["counts"]["valid_executed"] == 3
    assert data["summary"]["completeness"] == "complete"
    assert result.details["evidence_only"] is True
    assert result.details["next_report_command"][:4] == [
        "inferyard",
        "report",
        "--run",
        result.evidence_dir,
    ]
    assert data["run"]["execution_mode"] == "single"
    assert data["run"]["origin"] == "measured"
    assert data["run"]["schema_version"] == 3
    assert json.loads(locking.STATE_PATH.read_text())["dirty"] is False
    events = [
        json.loads(line)
        for line in (Path(result.evidence_dir) / "events.jsonl").read_text().splitlines()
    ]
    idle = [event for event in events if event["event_type"] == "idle_observed"]
    assert idle and all(event["data"]["state"] == "idle" for event in idle)


def test_scorer_failure_preserves_execution_and_collects_remaining_cases(scenario):
    request, deps, calls, settings = scenario

    def broken(*args):
        raise RuntimeError("synthetic scorer failure")

    deps.scorer = broken
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 3, result
    data = rebuild(Path(result.evidence_dir))
    assert data["requests"][0]["execution_state"] == "completed"
    assert data["requests"][0]["quality_state"] == "unscorable"
    assert [r["execution_state"] for r in data["requests"][1:]] == ["completed"] * 2
    assert len(calls) == 8


def test_initial_busy_blocks_all_generation(scenario):
    request, deps, calls, settings = scenario
    settings["busy"] = True

    # Keep this orchestration test short; adapter deadline behavior is tested independently.
    async def immediately_unknown(*args, **kwargs):
        return False

    factory = deps.adapter

    def adapter(*args, **kwargs):
        instance = factory(*args, **kwargs)
        instance.wait_idle = immediately_unknown
        return instance

    deps.adapter = adapter
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 2
    assert calls == []
    assert json.loads(locking.STATE_PATH.read_text())["dirty"] is True
    assert rebuild(Path(result.evidence_dir))["summary"]["counts"]["not_executed"] == 3


def test_cancel_preserves_terminal_and_unexecuted(scenario):
    request, deps, calls, settings = scenario

    async def run():
        task = asyncio.create_task(execute_async(request, deps))
        while len(calls) < 6 and not task.done():
            await asyncio.sleep(0.005)
        task.cancel()
        return await task

    code, result = asyncio.run(run())
    assert code == 130, result
    data = rebuild(Path(result.evidence_dir))
    assert data["requests"][0]["execution_state"] == "cancelled"
    assert data["summary"]["counts"]["not_executed"] == 2
    assert len(calls) == 6


def test_formal_corpus_requires_content_bound_human_record():
    bundle = json.loads((Path(__file__).parents[2] / "bundles/zh-smoke.json").read_text())
    bundle.pop("review_provenance", None)
    bundle["review_records"] = []
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(bundle)
    bundle["review_records"] = [
        {
            "reviewer": "synthetic-reviewer",
            "reviewed_at": "2026-09-29T00:00:00Z",
            "content_sha256": content_hash(bundle),
            "conclusion": "approved",
        }
    ]
    require_review(bundle)
    bundle["cases"][0]["prompt"] += " changed"
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(bundle)


def test_warmup_failure_never_starts_formal_cases(scenario):
    request, deps, calls, settings = scenario
    settings["fail_index"] = 2
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 2 and len(calls) == 3
    assert rebuild(Path(result.evidence_dir))["summary"]["counts"]["not_executed"] == 3


def test_pid_change_between_requests_stops_without_new_generation(scenario):
    request, deps, calls, settings = scenario

    def guard(*args):
        if len(calls) == 6:
            raise PreflightError("service_process_identity_changed")

    deps.guard = guard
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 3 and len(calls) == 6
    assert rebuild(Path(result.evidence_dir))["summary"]["counts"]["not_executed"] == 2


def test_unconfirmed_residual_stops_and_keeps_dirty(scenario):
    request, deps, calls, settings = scenario
    factory = deps.adapter

    def adapter(*args, **kwargs):
        instance = factory(*args, **kwargs)

        async def idle(*args, **kwargs):
            return len(calls) < 6

        instance.wait_idle = idle
        return instance

    deps.adapter = adapter
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 3 and len(calls) == 6
    data = rebuild(Path(result.evidence_dir))
    assert data["summary"]["completeness"] == "incomplete"
    assert data["summary"]["counts"]["not_executed"] == 2
    assert json.loads(locking.STATE_PATH.read_text())["dirty"] is True


def test_scoring_all_wrong_still_exits_zero(scenario):
    from inferyard.analysis.scoring import score_case

    request, deps, calls, settings = scenario
    deps.scorer = lambda case, answer, policy: score_case(case, "", policy)
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0 and result.completeness == "complete"
    data = rebuild(Path(result.evidence_dir))
    assert all(q["rate"]["value"] == 0 for q in data["summary"]["quality"]["Q01"].values())


def test_rerun_new_id_parent_and_original_bytes_unchanged(scenario, monkeypatch):
    request, deps, calls, settings = scenario
    code, original = asyncio.run(execute_async(request, deps))
    assert code == 0
    root = Path(original.evidence_dir)
    before = {p.name: p.read_bytes() for p in root.iterdir() if p.is_file()}
    ticks = request.config.config.to_dict()["endpoint"]["process_start_ticks"]
    # F16 process replacement is synthetic here; actual new-process rerun remains G2.
    monkeypatch.setattr(running, "process_start_ticks", lambda pid: ticks + 1)
    rerun = CommandRequest(
        "run", from_run=root, server_pid=os.getpid(), endpoint_url="http://127.0.0.1:9090"
    )
    code, second = asyncio.run(execute_async(rerun, deps))
    assert code == 0, second
    assert second.run_id != original.run_id
    meta = json.loads((Path(second.evidence_dir) / "run.json").read_text())
    assert meta["parent_run_id"] == original.run_id
    assert before == {p.name: p.read_bytes() for p in root.iterdir() if p.is_file()}
    assert len(calls) == 16


def test_credential_echo_all_artifacts_redacted_and_not_scored(scenario, monkeypatch):
    request, deps, calls, settings = scenario
    secret = "fixture-secret-7f39"
    monkeypatch.setenv("LAB_BENCH_FIXTURE_TOKEN", secret)
    config = request.config.config.to_dict()
    config["endpoint"]["api_key_env"] = "LAB_BENCH_FIXTURE_TOKEN"
    request = replace(
        request, config=replace(request.config, config=Document.parse("config", config))
    )
    settings["answer"] = secret
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 3, result
    root = Path(result.evidence_dir)
    assert all(
        secret.encode() not in path.read_bytes() for path in root.iterdir() if path.is_file()
    )
    data = rebuild(root)
    assert data["requests"][0]["quality_state"] == "unscorable"
    assert data["requests"][0]["score"]["reason"] == "redacted_scoring_input"
    assert secret not in str(result)


def test_io_failure_mid_answer_stops_without_counting_model_failure(scenario, monkeypatch):
    from inferyard.evidence.journal import TrialJournal
    from inferyard.evidence.storage import EvidenceError

    request, deps, calls, settings = scenario
    original = TrialJournal.event
    injected = False

    def event(self, kind, phase, key, data, **kwargs):
        nonlocal injected
        if phase == "formal" and kind == "content" and not injected:
            injected = True
            raise EvidenceError("fixture_ENOSPC")
        return original(self, kind, phase, key, data, **kwargs)

    monkeypatch.setattr(TrialJournal, "event", event)
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 4 and injected and len(calls) == 6
    data = rebuild(Path(result.evidence_dir))
    assert data["summary"]["counts"]["invalid"] == 1
    assert data["summary"]["counts"]["failed"] == 0
    assert data["summary"]["counts"]["not_executed"] == 2
    assert json.loads(locking.STATE_PATH.read_text())["dirty"]


@pytest.mark.parametrize(
    "section,key,value",
    [
        ("output", "root", "other-output"),
        ("endpoint", "url", "http://127.0.0.1:9090"),
        ("model", "sha256", "a" * 64),
        ("device", "id", "OTHER-DEVICE-LABEL"),
    ],
)
def test_fixed_lock_prevents_generation_despite_changed_labels(scenario, section, key, value):
    request, deps, calls, settings = scenario
    config = request.config.config.to_dict()
    if section == "output":
        value = str(Path(config["output"]["root"]).parent / value)
    config[section][key] = value
    request = replace(
        request, config=replace(request.config, config=Document.parse("config", config))
    )
    with locking.HostLock():
        code, result = asyncio.run(execute_async(request, deps))
    assert code == 2 and calls == []
    assert "host_lock_unavailable" in result.limitations


def test_real_cli_dispatch_serial_run_emits_only_one_json(scenario, monkeypatch, capsys):
    import inferyard.cli as cli
    from inferyard.analysis.scoring import score_case

    request, deps, calls, settings = scenario
    deps.scorer = lambda case, answer, policy: score_case(case, "", policy)
    monkeypatch.setattr(cli, "load_config", lambda path: request.config)
    monkeypatch.setattr(running, "Dependencies", lambda **options: deps)
    code = cli.main(["run", "--config", "synthetic-config"])
    output = capsys.readouterr()
    assert code == 0 and output.err == ""
    assert len(output.out.splitlines()) == 1
    assert json.loads(output.out)["completeness"] == "complete"
    assert len(calls) == 8


@pytest.mark.parametrize("changed_source", [False, True])
def test_rerun_rejects_old_service_or_changed_implementation(scenario, monkeypatch, changed_source):
    request, deps, calls, settings = scenario
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0
    if changed_source:
        from inferyard import implementation_identity

        original_version = implementation_identity.version
        monkeypatch.setattr(
            implementation_identity,
            "version",
            lambda name: "changed" if name == "httpx" else original_version(name),
        )
    rerun = CommandRequest(
        "run",
        from_run=Path(result.evidence_dir),
        server_pid=os.getpid(),
        endpoint_url="http://127.0.0.1:9090",
    )
    expected = (
        "rerun_tool_source_changed_or_unknown" if changed_source else "rerun_requires_new_service"
    )
    with pytest.raises(PreflightError, match=expected):
        running.load_rerun(rerun)
    assert len(calls) == 8


def test_check_uses_shared_trial_envelope_without_formal_grades(scenario):
    request, deps, calls, _ = scenario
    code, result = asyncio.run(execute_async(replace(request, command="check"), deps))
    assert code == 0 and len(calls) == 2
    data = rebuild(Path(result.evidence_dir))
    assert data["run"]["kind"] == "check"
    assert data["run"]["execution_mode"] == "single"
    assert data["summary"]["counts"]["executed"] == 0
    assert data["summary"]["counts"]["not_executed"] == 3
    assert data["summary"]["completeness"] == "incomplete"
    assert all(row["score"] is None for row in data["requests"])


def test_rerun_rejects_check_evidence_before_process_query(scenario, monkeypatch):
    request, deps, calls, _ = scenario
    code, result = asyncio.run(execute_async(replace(request, command="check"), deps))
    assert code == 0 and len(calls) == 2
    root = Path(result.evidence_dir)
    assert rebuild(root)["run"]["kind"] == "check"
    monkeypatch.setattr(
        running, "process_start_ticks", lambda pid: pytest.fail("unexpected process query")
    )
    rerun = CommandRequest(
        "run", from_run=root, server_pid=os.getpid(), endpoint_url="http://127.0.0.1:9090"
    )
    with pytest.raises(PreflightError, match="rerun_requires_single_run_source"):
        asyncio.run(execute_async(rerun, deps))
    assert len(calls) == 2
