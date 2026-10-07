import asyncio

from inferyard.config.plan_math import plan_hash
from inferyard.config.planning import compile_plan
from inferyard.evidence.ledger import read_trial
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_runner import scenario as runner_scenario
from tests.integration.test_trial_runner import inputs

scenario = runner_scenario


def duration_inputs(scenario, limit=100):
    plan, loaded, deps, output = inputs(scenario)
    source = plan["experiment"]
    workload = source["workloads"][0]
    workload["purpose"] = "stability"
    workload["repeats"] = 1
    workload["protocol"].update(
        kind="duration",
        duration_seconds=0.5,
        max_requests=limit,
        window_seconds=0.5,
        min_completed_per_case_per_window=1,
        drain_timeout_seconds=workload["timeout_seconds"],
    )
    return compile_plan(source), loaded, deps, output


def test_duration_cycles_requests_and_rebuilds_window(scenario, monkeypatch):
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
    assert len(data["requests"]) > 3
    assert len({r["request_id"] for r in data["requests"]}) == len(data["requests"])
    assert len({r["case_id"] for r in data["requests"]}) == 3
    assert [r["plan_index"] for r in data["requests"]] == list(range(len(data["requests"])))
    assert data["summary"]["duration"]["window_completed"]
    assert any(
        item["value"] is not None
        for item in data["summary"]["metric_observations"]
        if item["metric_id"] == "S02"
    )
    assert data["summary"]["counts"]["planned"] is None
    assert data["summary"]["quality"]["status"] == "repeated_probe_observations_only"
    assert data["summary"] == read_trial(root)["summary"]


def test_duration_request_limit_is_incomplete(scenario):
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
    assert read_json(root / "safety-check.json") == {
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
    from inferyard.evidence.storage import read_json
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
    assert read_json(root / "safety-check.json")["last"]["intel_pstate_no_turbo"]["value"] == 0
    assert data["summary"] == read_trial(root)["summary"]
