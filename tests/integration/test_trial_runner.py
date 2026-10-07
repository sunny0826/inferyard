"""Full preflight/probe/warmup lifecycle, with a real parser and synthetic service."""

import asyncio
import sys
from pathlib import Path

import pytest

import inferyard.runtime.lock as locking
from inferyard.analysis.scoring import score_case
from inferyard.config.planning import compile_plan
from inferyard.evidence.storage import read_json, sha256_file
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_runner import scenario as runner_scenario
from tests.unit.test_phase2_contracts import experiment

scenario = runner_scenario


@pytest.mark.skipif(sys.platform != "linux", reason="Real Linux CPU and /proc resource counters")
def test_resource_collector_records_boundaries_and_rebuilds_metrics(scenario):
    from inferyard.evidence.ledger import read_trial
    from inferyard.evidence.storage import read_jsonl
    from inferyard.platforms.resources_linux import ResourceSampler

    plan, loaded, deps, output = inputs(scenario)
    deps.sampler = ResourceSampler
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    samples = [s for s in data["samples"] if s["metric_name"] == "service_cpu_ticks"]
    assert {s["capture"] for s in samples} >= {"periodic", "request_start", "request_end", "idle"}
    endpoint = loaded.config.to_dict()["endpoint"]
    assert all(s["server_pid"] == endpoint["server_pid"] for s in samples)
    schedule, _ = read_jsonl(root / "schedule.jsonl")
    boundaries = [s for s in schedule if s.get("kind") == "resource_boundary"]
    assert all(b["read_finished_ns"] >= b["read_started_ns"] for b in boundaries)
    assert read_json(root / "collector.json")["collector"] == "linux-resource.v2"
    values = [
        m
        for m in data["summary"]["metric_observations"]
        if m["metric_id"] == "C04" and m["statistic"] == "observed_cpu_seconds"
    ]
    assert len(values) == 3 and all(m["value"] is not None for m in values)
    assert all(0 < m["coverage_ratio"] < 1 for m in values)
    context = data["summary"]["measurement_context"]
    assert not context["performance_comparison_eligible"]
    assert context["collector_schedule"]["periodic_samples"] > 0
    raw_external, issues = read_jsonl(root / "external-cpu.jsonl")
    assert not issues and raw_external
    assert context["external_cpu"]["sample_count"] == len(raw_external)
    assert all(r["server_pid"] == endpoint["server_pid"] for r in raw_external)
    assert not context["external_cpu"]["comparison_eligible"]
    assert not context["external_cpu_requests"]["all_requests_eligible"]
    assert context["external_cpu_requests"]["policy"] is None
    assert context["collector_schedule"]["boundary_samples"] == len(boundaries)
    assert context["collector_schedule"]["overhead_gate"] == "not_verified"
    assert {ref["path"] for ref in context["evidence_refs"]} == {
        "environment.start.json",
        "environment.end.json",
        "environment.jsonl",
        "schedule.jsonl",
        "external-cpu.jsonl",
    }
    assert all(sha256_file(root / ref["path"]) == ref["sha256"] for ref in context["evidence_refs"])
    assert data["summary"] == read_trial(root)["summary"]


@pytest.mark.parametrize(
    "timings",
    [
        {"cache_n": 0, "prompt_n": 1, "prompt_ms": 1, "predicted_n": 2, "predicted_ms": 2},
        {"sensitive": "fixture-secret-not-to-persist"},
    ],
)
def test_engine_timings_rebuild_offline_with_bound_build_and_unchanged_evidence(scenario, timings):
    from inferyard.evidence.ledger import read_trial

    scenario[3]["timings"] = timings
    plan, loaded, deps, output = inputs(scenario)
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    assert all(r["engine_build_verified"] for r in data["requests"])
    before = {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
    rebuilt = read_trial(root)
    assert rebuilt["summary"] == data["summary"]
    assert before == {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
    metrics = [
        m
        for m in rebuilt["summary"]["metric_observations"]
        if m["metric_id"] == "L07" and m["request_id"]
    ]
    assert len(metrics) == 3
    for item in metrics:
        assert not item["comparison_eligible"]  # diagnostic trial
        assert item["value"] == (500 if "predicted_n" in timings else None)
        assert item["missing_reason"] == (
            None if "predicted_n" in timings else "invalid_engine_timings"
        )
        assert item["sampled_start_ns"] is None  # engine phase clock is not client wall clock
        for ref in item["evidence_refs"]:
            assert sha256_file(root / ref["path"]) == ref["sha256"]
    assert "service.props.json" in read_json(root / "manifest.json")["files"]
    assert "fixture-secret-not-to-persist" not in (root / "events.jsonl").read_text()


def inputs(scenario):
    request, deps, calls, settings = scenario
    deps.scorer = score_case
    config = request.config.config.to_dict()
    source = experiment()
    source["budget"]["max_wall_seconds"] = 100_000
    source["workloads"][0]["timeout_seconds"] = config["execution"]["timeout_seconds"]
    source["workloads"][0]["protocol"]["case_ids"] = [
        c["case_id"] for c in request.config.bundle.to_dict()["cases"]
    ]
    return compile_plan(source), request.config, deps, Path(config["output"]["root"])


def test_full_diagnostic_trial_probes_warmups_and_formal_are_distinct(scenario):
    plan, loaded, deps, output = inputs(scenario)
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    assert len(scenario[2]) == 8
    assert data["summary"]["counts"]["completed"] == 3
    assert data["summary"]["scope_complete"]
    assert data["summary"]["completeness"] == "incomplete"
    assert data["summary"]["evidence_complete"]
    assert (root / "identity.json").exists()
    assert (root / "effective-stream.json").exists()
    assert not locking.read_json(locking.STATE_PATH)["dirty"]


def test_unreviewed_formal_bundle_is_blocked_before_any_request(scenario):
    plan, loaded, deps, output = inputs(scenario)
    code, data, _ = asyncio.run(
        run_trial(plan, plan["trials"][0]["trial_id"], loaded, output, dependencies=deps)
    )
    assert code == 2
    assert not scenario[2]
    assert data["summary"]["counts"]["not_executed"] == 3


def test_cancel_full_trial_preserves_remaining_cases_and_cleans_idle(scenario):
    plan, loaded, deps, output = inputs(scenario)

    async def run():
        task = asyncio.create_task(
            run_trial(
                plan,
                plan["trials"][0]["trial_id"],
                loaded,
                output,
                diagnostic=True,
                dependencies=deps,
            )
        )
        while len(scenario[2]) < 6 and not task.done():
            await asyncio.sleep(0.001)
        task.cancel()
        return await task

    code, data, _ = asyncio.run(run())
    assert code == 130
    assert data["summary"]["counts"]["cancelled"] == 1
    assert data["summary"]["counts"]["not_executed"] == 2
    assert not locking.read_json(locking.STATE_PATH)["dirty"]


@pytest.mark.parametrize("failure", ["probe", "warmup", "busy", "host_lock"])
def test_preformal_failures_never_start_quality_requests(scenario, failure):
    plan, loaded, deps, output = inputs(scenario)
    settings = scenario[3]
    if failure == "probe":
        settings["fail_index"] = 0
    elif failure == "warmup":
        settings["fail_index"] = 2
    elif failure == "busy":
        factory = deps.adapter

        def adapter(*args, **kwargs):
            instance = factory(*args, **kwargs)

            async def idle(*args, **kwargs):
                return False

            instance.wait_idle = idle
            return instance

        deps.adapter = adapter

    async def run():
        return await run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )

    if failure == "host_lock":
        with deps.lock():
            code, data, _ = asyncio.run(run())
    else:
        code, data, _ = asyncio.run(run())
    assert code == 2
    assert data["summary"]["counts"]["not_executed"] == 3
    assert len(scenario[2]) == {"probe": 1, "warmup": 3, "busy": 0, "host_lock": 0}[failure]


def test_memory_safety_stop_before_generation(scenario, monkeypatch):
    import inferyard.runtime.trial_runner as runner

    plan, loaded, deps, output = inputs(scenario)
    monkeypatch.setattr(runner, "memory_available", lambda: 0)
    code, data, _ = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 2 and not scenario[2]
    assert data["summary"]["stop_reason"] == "memory_safety_budget_reached"


def test_wall_budget_stops_during_management_before_formal(scenario):
    from dataclasses import replace

    from inferyard.config.plan_math import plan_hash
    from inferyard.contracts.validation import Document

    plan, loaded, deps, output = inputs(scenario)
    config = loaded.config.to_dict()
    config["execution"]["timeout_seconds"] = 0.01
    loaded = replace(loaded, config=Document.parse("config", config))
    definition = plan["experiment"]
    definition["workloads"][0].update(timeout_seconds=0.01, overhead_budget_seconds=0.01)
    plan = compile_plan(definition)
    assert plan["plan_sha256"] == plan_hash(plan)
    factory = deps.adapter

    def adapter(*args, **kwargs):
        instance = factory(*args, **kwargs)

        async def slow_properties(*args):
            await asyncio.sleep(1)

        instance.verify_properties = slow_properties
        return instance

    deps.adapter = adapter
    code, data, _ = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 3 and not scenario[2]
    assert data["summary"]["stop_reason"] == "trial_wall_budget_exhausted"
    assert data["summary"]["counts"]["not_executed"] == 3


def test_corpus_scoring_is_connected_but_review_gate_remains(scenario):
    from dataclasses import replace

    from inferyard.contracts.validation import Document

    plan, loaded, deps, output = inputs(scenario)
    bundle = loaded.bundle.to_dict()
    bundle.update(schema_version=3, task_protocol="quality", review_records=[])
    bundle["cases"][2] = dict(
        case_id=bundle["cases"][2]["case_id"],
        category="qa",
        prompt="只输出北京",
        reference_answer="北京",
        rules=dict(answers=["北京"], normalization="strip"),
    )
    loaded = replace(loaded, bundle=Document.parse("bundle", bundle))
    code, blocked, _ = asyncio.run(
        run_trial(plan, plan["trials"][0]["trial_id"], loaded, output, dependencies=deps)
    )
    assert code == 2 and not scenario[2]
    assert blocked["summary"]["counts"]["not_executed"] == 3
    code, data, _ = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0 and len(scenario[2]) == 8
    assert data["requests"][2]["category"] == "qa"
    assert data["requests"][2]["score"]["quality_state"] == "pass"
    assert data["summary"]["completeness"] == "incomplete"


def test_budget_cancellation_is_not_mislabeled_as_user_cancellation(scenario):
    plan, loaded, deps, output = inputs(scenario)
    factory = deps.adapter

    def adapter(*args, **kwargs):
        instance = factory(*args, **kwargs)
        original = instance.generate

        async def generate(*args, **kwargs):
            if len(scenario[2]) == 5:
                await asyncio.sleep(2)
            return await original(*args, **kwargs)

        instance.generate = generate
        return instance

    deps.adapter = adapter
    code, data, _ = asyncio.run(
        run_trial(
            plan,
            plan["trials"][0]["trial_id"],
            loaded,
            output,
            diagnostic=True,
            dependencies=deps,
            wall_budget_seconds=0.7,
        )
    )
    assert code == 3
    assert data["summary"]["stop_reason"] == "trial_wall_budget_exhausted"
    assert data["requests"][0]["error_category"] == "trial_wall_budget_exhausted_before_send"
    assert data["summary"]["counts"]["cancelled"] == 1


@pytest.mark.parametrize("target,expected", [(1, "matched"), (1024, "mismatch")])
def test_input_target_checked_before_generation_and_rebuilt_offline(scenario, target, expected):
    from inferyard.evidence.ledger import read_trial

    plan, loaded, deps, output = inputs(scenario)
    source = plan["experiment"]
    source["workloads"][0].update(input_target_tokens=target, input_tolerance_tokens=0)
    plan = compile_plan(source)
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert data["summary"]["input_target_check"]["status"] == expected
    assert read_json(root / "input-target-check.json")["status"] == expected
    assert read_trial(root)["summary"] == data["summary"]
    if expected == "mismatch":
        assert code == 2
        assert not scenario[2]
        assert data["summary"]["counts"]["not_executed"] == 3
    else:
        assert code == 0
        assert data["summary"]["counts"]["completed"] == 3


@pytest.mark.parametrize("target", [1, 2])
def test_length_preparation_only_uses_management_calls(scenario, tmp_path, target):
    from dataclasses import replace

    from inferyard.evidence.storage import json_bytes
    from inferyard.runtime.length_prepare import prepare
    from tests.unit.test_length_builder import spec

    request, deps, calls, _ = scenario
    source = tmp_path / "length-spec.json"
    source.write_bytes(json_bytes(spec(target_tokens=target, max_probes=3)))
    out = tmp_path / "length-prepared"
    request = replace(request, command="prepare-length", length_spec=source, out=out)
    code, result = asyncio.run(prepare(request, deps))
    if target == 1:
        assert code == 0 and result.status == "matched"
        assert (out / "prompt.txt").read_text() == "开始结束"
    else:
        assert code == 3 and result.status == "search_exhausted"
        assert not (out / "prompt.txt").exists()
        assert len(read_json(out / "probes.json")) == 3
    assert calls == []
    manifest = read_json(out / "preparation-manifest.json")
    assert all(sha256_file(out / name) == digest for name, digest in manifest["files"].items())
