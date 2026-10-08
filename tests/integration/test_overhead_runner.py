import asyncio

import pytest

from inferyard.evidence.storage import EvidenceError, json_bytes, read_json
from inferyard.platforms.resources import resource_collector_id
from inferyard.runtime.overhead_runner import read_overhead, run_overhead
from tests.integration.test_runner import scenario as runner_scenario
from tests.integration.test_trial_runner import inputs

scenario = runner_scenario


def test_abba_runs_real_lifecycle_and_preserves_frozen_protocol(scenario):
    plan, loaded, deps, output = inputs(scenario)
    root = output / "overhead"
    result = asyncio.run(
        run_overhead(
            plan,
            plan["trials"][0]["trial_id"],
            loaded,
            root,
            tolerance_ratio=0.05,
            max_wall_seconds=60,
            dependencies=deps,
            common_observer=True,
        )
    )
    protocol = read_json(root / "protocol.json")
    assert protocol["kind"] == "collector_overhead_abba.v2"
    trials = read_json(root / "trials.json")
    assert [row["mode"] for row in trials] == ["off", "on", "on", "off"]
    assert len({row["run_id"] for row in trials}) == 4
    assert all(row["protocol_sha256"] == protocol["protocol_sha256"] for row in trials)
    assert not result["performance_comparison_eligible"]
    assert result["status"] == "evaluated"
    rebuilt = read_overhead(root)
    assert rebuilt == {
        k: v for k, v in result.items() if k not in ("elapsed_seconds", "execution_exit_code")
    }
    targeted = read_overhead(root, target=root / trials[1]["path"])
    binding = targeted["target_binding"]
    assert not targeted["environment_binding"]["eligible"]
    assert any(reason.startswith("arm_1:") for reason in targeted["environment_binding"]["reasons"])
    assert binding["applicable"] == rebuilt["passed"]
    assert not binding["performance_comparison_eligible"]
    off_binding = read_overhead(root, target=root / trials[0]["path"])["target_binding"]
    assert not off_binding["applicable"]
    assert "target_collector_mismatch" in off_binding["reasons"]
    for row in trials:
        run = root / row["path"]
        collector = read_json(run / "collector.json")
        assert collector["collector"] == (
            "environment-only.v1" if row["mode"] == "off" else resource_collector_id()
        )
        assert bool((run / "memory.jsonl").read_text()) == (row["mode"] == "on")
        assert (run / "environment.start.json").exists()
        assert (run / "environment.end.json").exists()
        assert (run / "environment.jsonl").read_text()
        assert (run / "external-cpu.jsonl").read_text()
        assert read_json(run / "observer.json") == protocol["observer"]
    with pytest.raises(FileExistsError):
        asyncio.run(
            run_overhead(
                plan,
                plan["trials"][0]["trial_id"],
                loaded,
                root,
                tolerance_ratio=0.9,
                max_wall_seconds=60,
                dependencies=deps,
            )
        )
    # A self-consistent rewritten protocol/index cannot relabel the observed interval.
    from inferyard.runtime.collector_overhead import freeze_protocol

    keys = (
        "case_ids",
        "interval_ms",
        "tolerance_ratio",
        "max_wall_seconds",
        "config_sha256",
        "bundle_sha256",
        "tool_source_sha256",
    )
    changed = {key: protocol[key] for key in keys}
    changed["interval_ms"] += 1
    forged = freeze_protocol(
        **changed, common_observer=True, resource_collector=resource_collector_id()
    )
    (root / "protocol.json").write_bytes(json_bytes(forged))
    for row in trials:
        row["protocol_sha256"] = forged["protocol_sha256"]
    (root / "trials.json").write_bytes(json_bytes(trials))
    with pytest.raises(EvidenceError, match="overhead_interval_mismatch"):
        read_overhead(root)


def test_overhead_budget_stops_without_starting_later_arms(scenario):
    plan, loaded, deps, output = inputs(scenario)
    root = output / "budget-overhead"
    result = asyncio.run(
        run_overhead(
            plan,
            plan["trials"][0]["trial_id"],
            loaded,
            root,
            tolerance_ratio=0.05,
            max_wall_seconds=0.03,
            dependencies=deps,
        )
    )
    assert not result["passed"]
    assert result["status"] == "incomplete"
    assert len(read_json(root / "trials.json")) <= 1
    assert (root / "result.json").exists()


def test_overhead_cancel_retains_evidence_and_does_not_retry(scenario):
    plan, loaded, deps, output = inputs(scenario)
    root = output / "cancel-overhead"

    async def run():
        task = asyncio.create_task(
            run_overhead(
                plan,
                plan["trials"][0]["trial_id"],
                loaded,
                root,
                tolerance_ratio=0.05,
                max_wall_seconds=60,
                dependencies=deps,
            )
        )
        while len(scenario[2]) < 6 and not task.done():
            await asyncio.sleep(0.001)
        task.cancel()
        return await task

    result = asyncio.run(run())
    assert result["execution_exit_code"] == 130
    assert not result["passed"]
    trials = read_json(root / "trials.json")
    assert len(trials) == 1
    assert (root / trials[0]["path"] / "manifest.json").exists()
    assert read_overhead(root)["status"] == "incomplete"


@pytest.mark.parametrize(
    "mode", ["common", "boundary", "first_events", "engine_rates", "block_gaps", "resources"]
)
def test_observers_can_qualify_all_four_environment_windows(scenario, monkeypatch, mode):
    """Synthetic stable environment and idle host counters, real lifecycle/ledger."""
    import sys
    from copy import deepcopy
    from dataclasses import replace

    from inferyard.config.planning import compile_plan
    from inferyard.contracts.validation import Document
    from inferyard.platforms.external_cpu import SOURCE
    from tests.unit.test_environment import state

    if mode == "resources" and sys.platform != "linux":
        pytest.skip("The source-qualified Linux resource arm requires native /proc counters")

    plan, loaded, deps, output = inputs(scenario)
    config = loaded.config.to_dict()
    config["telemetry"]["interval_ms"] = 250 if mode == "resources" else 1000
    if mode == "resources":
        # Keep three baseline intervals and five request intervals while allowing
        # native collection and synchronous evidence writes on shared CI hosts.
        config["telemetry"]["baseline_seconds"] = 0.75
        scenario[3]["stream_delay"] = 1.25
    loaded = replace(loaded, config=Document.parse("config", config))
    definition = plan["experiment"]
    definition["definition_versions"]["measurement"] = "phase2.v1"
    definition["performance_environment"] = {
        "max_external_cpu_percent": 25,
        "max_external_interval_seconds": 1,
    }
    if mode == "resources":
        definition["resource_comparison"] = {
            "min_coverage_ratio": 0.2,
            "max_window_offset_difference": 0.5,
            "max_sample_gap_intervals": 2,
            "min_baseline_samples": 2,
        }
    plan = compile_plan(definition)
    snapshot = state()
    snapshot["platform"] = "synthetic-test-platform"
    snapshot.update(
        {key: config["conditions"][key] for key in ("ac_online", "profile", "governor", "epp")}
    )
    for policy in snapshot["cpu_policies"]["policies"]:
        policy["scaling_governor"] = config["conditions"]["governor"]
        policy["energy_performance_preference"] = config["conditions"]["epp"]
    deps.preflight = lambda config: (
        {
            "origin": "http://127.0.0.1:8080",
            "environment": deepcopy(snapshot),
            "verification": "verified",
        },
        [],
    )
    deps.environment = lambda: deepcopy(snapshot)
    monkeypatch.setattr("inferyard.platforms.telemetry.environment_snapshot", deps.environment)
    monkeypatch.setattr(
        "inferyard.runtime.environment_schedule.environment_snapshot", deps.environment
    )
    monkeypatch.setattr(
        "inferyard.runtime.boundary_observer.environment_snapshot", deps.environment
    )

    def idle_cpu(pid, ticks, boot, hz, phase, request, clock, root=None):
        at = clock()
        return {
            "source": SOURCE,
            "server_pid": pid,
            "process_start_ticks": ticks,
            "boot_id": "synthetic-test-boot",
            "clock_ticks_per_second": 1_000_000_000,
            "phase": phase,
            "request_id": request,
            "read_started_ns": at,
            "read_finished_ns": at,
            "host_ticks": [0, 0, 0, at, 0, 0, 0, 0],
            "service_ticks": 0,
            "missing_reason": None,
        }

    monkeypatch.setattr("inferyard.runtime.environment_observer.capture_external_cpu", idle_cpu)
    monkeypatch.setattr("inferyard.runtime.boundary_observer.capture", idle_cpu)
    if mode == "engine_rates":
        scenario[3]["timings"] = {
            "cache_n": 0,
            "prompt_n": 10,
            "prompt_ms": 20,
            "predicted_n": 2,
            "predicted_ms": 10,
        }
    if mode == "block_gaps":
        scenario[3].update(answer="x" * 21, split_content=True)
    root = output / "qualified-environment"
    asyncio.run(
        run_overhead(
            plan,
            plan["trials"][0]["trial_id"],
            loaded,
            root,
            tolerance_ratio=0.99,
            max_wall_seconds=60,
            dependencies=deps,
            common_observer=mode == "common",
            boundary_observer=mode
            in ("boundary", "first_events", "engine_rates", "block_gaps", "resources"),
            first_event_tolerance_ratio=0.99 if mode == "first_events" else None,
            engine_rate_tolerance_ratio=0.05 if mode == "engine_rates" else None,
            block_gap_tolerance_ms=100 if mode == "block_gaps" else None,
        )
    )
    trials = read_json(root / "trials.json")
    assert len(trials) == 4
    result = read_overhead(root, target=root / trials[1]["path"])
    assert result["environment_binding"]["eligible"], result["environment_binding"]["reasons"]
    assert all(row["eligible"] for row in result["environment_binding"]["runs"])
    assert not result["performance_comparison_eligible"]
    if mode in ("boundary", "first_events", "engine_rates", "block_gaps", "resources"):
        from inferyard.evidence.ledger import read_trial
        from inferyard.evidence.storage import read_jsonl

        for arm in trials:
            path = root / arm["path"]
            data = read_trial(path)
            records, issues = read_jsonl(path / "request-environment.jsonl")
            assert not issues
            for request in data["requests"]:
                pair = [r for r in records if r["request_id"] == request["request_id"]]
                assert len(pair) == 2
                assert pair[0]["read_finished_ns"] <= request["t_send_ns"]
                assert pair[1]["read_started_ns"] >= request["t_terminal_ns"]
            if arm["mode"] == "off":
                assert not data["samples"]
                assert not (path / "environment.jsonl").read_text()
        from inferyard.evidence.total_applicability import load_performance_evidence_v3
        from inferyard.platforms.resources import ResourceSampler

        # An ABBA arm is not an independent subsequent target.
        from inferyard.reporting.comparison_report import (
            build_comparison,
            comparison_input,
            verify_comparison,
        )
        from inferyard.runtime.trial_runner import run_trial

        target = root / trials[1]["path"]
        target_data, reference = comparison_input(target)
        circular = load_performance_evidence_v3(root, target, data=target_data, reference=reference)
        assert not circular["eligible"]
        assert circular["assessment"] == {}
        assert "formal_trial_total_observer_evidence_missing" in circular["reasons"]
        # Isolate incremental per-metric gates in this synthetic fixture.
        # The actual full-trial path and missing-proof rejection have separate regressions.
        monkeypatch.setattr(
            "inferyard.evidence.total_applicability.load_total_applicability",
            lambda *args, **kwargs: {"eligible": True, "reasons": [], "tolerance_ratio": 0.99},
        )
        # Only this synthetic test bypasses human corpus admission.
        monkeypatch.setattr("inferyard.runtime.trial_runner.require_review", lambda bundle: None)
        deps.sampler = ResourceSampler
        targets = []
        for index in range(2):
            code, data, path = asyncio.run(
                run_trial(
                    plan,
                    plan["trials"][0]["trial_id"],
                    loaded,
                    output / f"formal-target-{index}",
                    dependencies=deps,
                )
            )
            assert code == 0 and data["summary"]["completeness"] == "complete"
            targets.append(path)
        comparison = build_comparison(
            *targets, left_overhead=root, right_overhead=root, format_version=4
        )
        assert comparison["eligibility"]["performance"], comparison["performance_analysis"][
            "blockers"
        ]
        eligible = [r for r in comparison["performance_analysis"]["differences"] if r["eligible"]]
        allowed = {"L01", "L02", "L03", "L04"} if mode == "first_events" else {"L03", "L04"}
        if mode == "engine_rates":
            allowed |= {"L06", "L07"}
            assert all(r["passed"] for r in result["engine_rate_assessments"].values())
            assert {"L06", "L07"} <= {r["metric_id"] for r in eligible}
            assert {"L06", "L07"} <= {
                r["metric_id"] for r in eligible if r["case_id"] is None and r["statistic"] == "p50"
            }
        if mode == "block_gaps":
            allowed.add("L05")
            assert all(r["passed"] for r in result["block_gap_assessments"].values())
            qualified = [r for r in eligible if r["metric_id"] == "L05"]
            assert len(qualified) == 12
            assert all(r["case_id"] is not None for r in qualified)
        if mode == "resources":
            allowed |= {f"C0{i}" for i in range(1, 9)}
            assert {"C01", "C02", "C04", "C05"} <= {r["metric_id"] for r in eligible}
            assert all(r["resource_windows"] for r in eligible if r["metric_id"].startswith("C"))
        assert eligible and {r["metric_id"] for r in eligible} <= allowed
        if mode == "first_events":
            assert all(r["passed"] for r in result["first_event_assessments"].values())
            assert {"L01", "L02"} <= {r["metric_id"] for r in eligible}
        out = output / "performance-comparison"
        from inferyard.application.types import CommandRequest
        from inferyard.reporting.comparison_report import execute as write_comparison

        write_comparison(
            CommandRequest(
                "compare",
                left=targets[0],
                right=targets[1],
                out=out,
                left_overhead=root,
                right_overhead=root,
            )
        )
        assert verify_comparison(out)["verified"]
        from inferyard.reporting.report import verify_report, write_report

        report = output / "linked-performance-report"
        index = write_report(targets, report, comparison_path=out)
        assert index["report_format_version"] == 7
        assert index["comparison"]["eligibility"]["performance"]
        assert verify_report(report)["verified"]
        html = (report / "report.html").read_text()
        assert 'class="performance-deltas"' in html
        assert 'data-bound-pair="true"' in html
        assert "获准行数不代表独立题数" in html
        if mode == "resources":
            assert 'class="resource-window"' in html and "覆盖率是样本时间跨度" in html
        with pytest.raises(EvidenceError, match="sources_or_order_mismatch"):
            write_report(list(reversed(targets)), output / "wrong-order", comparison_path=out)
        with pytest.raises(EvidenceError, match="requires_two_runs"):
            write_report(targets[:1], output / "wrong-scope", comparison_path=out)
        with pytest.raises(EvidenceError, match="inside_original_evidence"):
            write_report(targets, root / "forbidden-report", comparison_path=out)
        from inferyard.analysis.candidate_filter import filter_candidates

        metric = next(
            m
            for m in data["summary"]["metric_observations"]
            if m["metric_id"] == "L03" and m["statistic"] == "p50"
        )
        constraint = {
            k: metric[k] for k in ("metric_id", "statistic", "source", "unit", "definition_version")
        }
        constraint.update(
            category=metric["group"]["category"],
            error_category=None,
            operator="<=",
            threshold=metric["value"],
        )
        spec = {
            "version": 1,
            "reference": str(targets[0]),
            "candidates": [str(targets[1])],
            "constraints": [constraint],
            "comparisons": {str(targets[1]): str(out)},
        }
        if mode == "resources":
            rss = [m for m in data["summary"]["metric_observations"] if m["metric_id"] == "C02"]
            resource_constraint = {
                k: rss[0][k]
                for k in ("metric_id", "statistic", "source", "unit", "definition_version")
            }
            resource_constraint.update(
                category=None,
                error_category=None,
                operator="<=",
                threshold=max(m["value"] for m in rss),
                aggregation="all_requests",
            )
            spec["constraints"].append(resource_constraint)
        filtered = filter_candidates(spec, output)
        assert filtered["schema_version"] == 3 and filtered["format_version"] == 1
        assert filtered["candidates"][0]["matched"]
        if mode == "resources":
            aggregate = filtered["candidates"][0]["constraints"][1]["aggregation"]
            assert aggregate["planned_requests"] == 3 and aggregate["counts"]["pass"] == 3
        from inferyard.analysis.candidate_filter import execute as execute_filter
        from inferyard.application.types import CommandRequest

        spec_file = output / "criteria.json"
        spec_file.write_bytes(json_bytes(spec))
        with pytest.raises(EvidenceError, match="inside_original_evidence"):
            execute_filter(
                CommandRequest(
                    command="filter-candidates",
                    filter_spec=spec_file,
                    out=root / "forbidden-filter",
                )
            )
        unlinked = {k: v for k, v in spec.items() if k != "comparisons"}
        assert not filter_candidates(unlinked, output)["matched_run_ids"]
        with pytest.raises(EvidenceError, match="sources_or_order_mismatch"):
            filter_candidates({**spec, "reference": str(targets[1])}, output)
        comparison["performance_evidence"][0]["eligible"] = False
        (out / "comparison.json").write_bytes(json_bytes(comparison))
        with pytest.raises(EvidenceError, match="presentation_bytes_changed"):
            verify_comparison(out)
        with pytest.raises(EvidenceError, match="presentation_bytes_changed"):
            verify_report(report)
        with pytest.raises(EvidenceError, match="presentation_bytes_changed"):
            filter_candidates(spec, output)
