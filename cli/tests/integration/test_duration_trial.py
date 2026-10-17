import asyncio

import pytest

from inferyard.config.plan_math import plan_hash
from inferyard.config.planning import compile_plan
from inferyard.contracts.validation import ContractError
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_lab_cli import lab_scenario  # noqa: F401
from tests.integration.test_native_cli import native_scenario as native_fixture
from tests.integration.test_runner import scenario as runner_scenario
from tests.integration.test_trial_runner import inputs

scenario = runner_scenario
native_scenario = native_fixture


def duration_inputs(scenario, limit=100):
    plan, loaded, deps, output = inputs(scenario)
    source = plan["experiment"]
    workload = source["workloads"][0]
    workload["purpose"] = "stability"
    workload["repeats"] = 1
    # A request-cap scenario must leave time for a full request before the window closes.
    duration_seconds = 2 * workload["timeout_seconds"] if limit == 1 else 0.5
    workload["protocol"].update(
        kind="duration",
        duration_seconds=duration_seconds,
        max_requests=limit,
        window_seconds=duration_seconds,
        min_completed_per_case_per_window=1,
        drain_timeout_seconds=workload["timeout_seconds"],
    )
    return compile_plan(source), loaded, deps, output


@pytest.mark.parametrize("stream_delay", [0.03, 0.6])
def test_duration_cycles_requests_and_rebuilds_window(scenario, monkeypatch, stream_delay):
    scenario[3]["stream_delay"] = stream_delay
    plan, loaded, deps, output = duration_inputs(scenario)
    from inferyard.platforms.resources_linux import ResourceSampler

    # The duration lifecycle uses synthetic idle RSS so it is independent of /proc.
    monkeypatch.setattr("inferyard.platforms.resources_linux.read_rss", lambda *args: (1024, None))
    deps.sampler = ResourceSampler
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0, data["summary"]["stop_reason"]
    assert data["requests"]
    order = data["selection"]["case_ids"]
    assert [r["case_id"] for r in data["requests"]] == [
        order[index % len(order)] for index in range(len(data["requests"]))
    ]
    assert all(r["execution_state"] == "completed" for r in data["requests"])
    assert len({r["request_id"] for r in data["requests"]}) == len(data["requests"])
    assert [r["plan_index"] for r in data["requests"]] == list(range(len(data["requests"])))
    assert data["summary"]["duration"]["window_completed"]
    assert data["summary"]["duration"]["probe_coverage_complete"] == (
        len(data["requests"]) >= len(order)
    )
    rss = [item for item in data["summary"]["metric_observations"] if item["metric_id"] == "S02"]
    assert rss
    if len(data["requests"]) >= len(order):
        assert any(item["value"] is not None for item in rss)
    else:
        assert all(
            item["value"] is None and item["missing_reason"] == "no_complete_probe_cycle"
            for item in rss
        )
    assert data["summary"]["counts"]["planned"] is None
    assert data["summary"]["quality"]["status"] == "repeated_probe_observations_only"
    assert data["summary"] == read_trial(root)["summary"]


@pytest.mark.parametrize("stream_delay", [0.03, 0.6])
def test_duration_request_limit_is_incomplete(scenario, stream_delay):
    scenario[3]["stream_delay"] = stream_delay
    plan, loaded, deps, output = duration_inputs(scenario, limit=1)
    # Plan budget always remains canonical after the fixture changes.
    assert plan["plan_sha256"] == plan_hash(plan)
    code, data, _ = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 3, data["summary"]["stop_reason"]
    assert len(data["requests"]) == 1
    assert data["summary"]["duration"]["reason"] == "request_limit_reached"
    assert not data["summary"]["scope_complete"]


def test_native_duration_is_rejected_before_request_admission(native_scenario):
    command, deps, service = native_scenario
    plan, loaded, deps, output = duration_inputs((command, deps, service.requests, service.options))
    with pytest.raises(ContractError, match="currently support fixed serial quality text"):
        asyncio.run(
            run_trial(
                plan,
                plan["trials"][0]["trial_id"],
                loaded,
                output,
                diagnostic=True,
                dependencies=deps,
            )
        )
    assert not service.requests and not service.calls
    assert not output.exists()


def test_cancel_duration_retains_interrupted_window(scenario):
    plan, loaded, deps, output = duration_inputs(scenario)

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
        while len(scenario[2]) < 7 and not task.done():
            await asyncio.sleep(0.001)
        task.cancel()
        return await task

    code, data, root = asyncio.run(run())
    assert code == 130
    assert not data["summary"]["duration"]["window_completed"]
    assert data["summary"]["duration"]["reason"] == "interrupted"
    assert data["summary"] == read_trial(root)["summary"]


def test_duration_report_rebuilds_completed_and_interrupted_windows(scenario, tmp_path):
    from inferyard.reporting.report import verify_report, write_report

    for limit in (1, 100):
        plan, loaded, deps, output = duration_inputs(scenario, limit=limit)
        _, data, root = asyncio.run(
            run_trial(
                plan,
                plan["trials"][0]["trial_id"],
                loaded,
                output,
                diagnostic=True,
                dependencies=deps,
            )
        )
        target = tmp_path / f"report-{limit}"
        index = write_report([root], target)
        view = index["runs"][0]["duration_view"]
        assert view["completed"] == data["summary"]["duration"]["window_completed"]
        assert len(view["cases"]) == 3
        assert index["runs"][0]["diagnostic"]
        assert "持续负载 · 窗口趋势" in (target / "report.html").read_text()
        assert verify_report(target)["semantic_verified"]


def test_runtime_safety_cancels_inflight_and_preserves_incomplete_window(scenario):
    from inferyard.evidence.storage import read_json
    from inferyard.platforms.identity import PreflightError

    plan, loaded, deps, output = duration_inputs(scenario)
    source = plan["experiment"]
    source["safety"] = dict(
        interval_seconds=0.1,
        max_temperature_celsius=90,
        require_temperature=True,
        max_external_cpu_percent=None,
        check_environment=False,
    )
    plan = compile_plan(source)

    class Guard:
        last = None

        def __init__(self, *args):
            pass

        def metadata(self):
            return {"policy": source["safety"]}

        def check(self, *, periodic=False):
            if len(scenario[2]) >= 5:
                scenario[3]["stream_delay"] = 1
            if periodic and len(scenario[2]) >= 6:
                self.last = {"temperature": 90}
                raise PreflightError("temperature_safety_threshold_reached")

    deps.safety = Guard
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 3
    assert data["summary"]["stop_reason"] == "temperature_safety_threshold_reached"
    assert data["summary"]["counts"]["cancelled"] == 1
    assert data["requests"][0]["error_category"] == "temperature_safety_threshold_reached"
    assert not data["summary"]["duration"]["window_completed"]
    assert len(scenario[2]) == 6
    assert TrialEvidence(root).observation("safety_check", stage="finalizing") == {
        "stop_reason": "temperature_safety_threshold_reached",
        "last": {"temperature": 90},
    }
    import json

    history = [json.loads(line) for line in (root / "safety-checks.jsonl").read_text().splitlines()]
    assert [row["sequence"] for row in history] == list(range(1, len(history) + 1))
    assert history[-1]["trigger"] == "periodic"
    assert history[-1]["error"]["reason"] == "temperature_safety_threshold_reached"
    assert history[-1]["observation"] == {"temperature": 90}
    assert "safety-checks.jsonl" in read_json(root / "manifest.json")["files"]
    assert data["summary"] == read_trial(root)["summary"]


def test_frozen_turbo_change_stops_real_safety_guard_inflight(scenario, tmp_path):
    from inferyard.runtime.safety import SafetyGuard
    from tests.unit.test_safety import Sensors

    path = tmp_path / "sys/devices/system/cpu/intel_pstate/no_turbo"
    path.parent.mkdir(parents=True)
    path.write_text("1\n")
    plan, loaded, deps, output = duration_inputs(scenario)
    source = plan["experiment"]
    source["safety"] = dict(
        interval_seconds=0.1,
        max_temperature_celsius=90,
        require_temperature=True,
        max_external_cpu_percent=None,
        check_environment=False,
        intel_pstate_no_turbo=1,
    )
    plan = compile_plan(source)

    class Guard(SafetyGuard):
        def __init__(self, policy, config, environment):
            super().__init__(
                policy, config, environment, sensors=Sensors(40), sys_root=tmp_path / "sys"
            )

        def check(self, *, periodic=False):
            if len(scenario[2]) >= 5:
                scenario[3]["stream_delay"] = 1
            if periodic and len(scenario[2]) >= 6:
                path.write_text("0\n")
            super().check(periodic=periodic)

    deps.safety = Guard
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 3
    assert data["summary"]["stop_reason"] == "intel_pstate_safety_condition_changed"
    assert data["summary"]["counts"]["cancelled"] == 1
    assert len(scenario[2]) == 6
    assert not data["summary"]["duration"]["window_completed"]
    assert (
        TrialEvidence(root).observation("safety_check", stage="finalizing")["last"][
            "intel_pstate_no_turbo"
        ]["value"]
        == 0
    )
    assert data["summary"] == read_trial(root)["summary"]
