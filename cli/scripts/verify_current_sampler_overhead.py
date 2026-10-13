"""Current-source, one-thread short workload; retain user temperature/EPP opt-outs.

Incremental resource sampler ABBA relative to common safety and boundary guards.
It does not isolate total safety logging cost or authorize other recipes.
"""

import argparse
import asyncio
import json
import socket
import subprocess
import time
import urllib.error
import urllib.request
from pathlib import Path

from inferyard.config.environment_binding import mismatches
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.config.planning import write_plan
from inferyard.evidence.storage import atomic_bytes, json_bytes, read_json, sha256_file
from inferyard.platforms.identity import environment_snapshot, process_start_ticks
from inferyard.platforms.sensors_linux import LinuxSensors
from inferyard.reporting.comparison_report import build_comparison, verify_comparison
from inferyard.runtime.overhead_runner import read_overhead, run_overhead
from inferyard.runtime.runner import require_review
from inferyard.runtime.trial_runner import run_trial


def save(path, value):
    atomic_bytes(path, json_bytes(value))


def announce(stage, **details):
    print(json.dumps({"stage": stage, **details}), flush=True)


def prepare(out, *, source, interval_ms=1000, fixed_output=False):
    config = read_json(source / "config.json")
    bundle = read_json(source / "bundle.json")
    config["generation"]["max_tokens"] = 32
    config["execution"]["timeout_seconds"] = 30
    config["telemetry"]["interval_ms"] = interval_ms
    require_review(bundle)
    if fixed_output:
        bundle = {
            **bundle,
            "schema_version": 3,
            "bundle_id": "current-sampler-fixed32-diagnostic",
            "task_protocol": "performance",
            "review_records": [],
            "cases": [
                {**case, "category": "performance", "rules": {"output_target_tokens": 32}}
                for case in bundle["cases"][:3]
            ],
        }
    for key, flag in (("threads", "-t"), ("threads_batch", "-tb")):
        config["conditions"][key] = 1
        args = config["engine"]["startup_args"]
        args[args.index(flag) + 1] = "1"
    environment = environment_snapshot()
    if mismatches(config["conditions"], environment):
        raise RuntimeError("frozen_environment_mismatch")
    temperatures = [
        r for r in LinuxSensors().collect("preparation", None) if r["metric_name"] == "temperature"
    ]
    save(
        out / "preparation.json",
        {
            "environment": environment,
            "temperature_samples": temperatures,
            "threads": 1,
            "threads_batch": 1,
            "interval_ms": interval_ms,
            "case_ids": [c["case_id"] for c in bundle["cases"][:3]],
            "scope": (
                "derived_unreviewed_fixed32_product_diagnostic_no_quality_acceptance"
                if fixed_output
                else "approved_three_case_subset_not_full_phase2_acceptance"
            ),
            "policy": (
                "5 percent overhead; temperature/EPP observations only; "
                "external CPU 25 percent; no retries"
            ),
            "safety_monitor": "same frozen safety monitoring in all arms and targets",
        },
    )
    return config, bundle


def freeze(out, config, bundle, *, fixed_output=False, case_ids=None, comparison=None, repeats=2):
    if type(repeats) is not int or repeats < 1:
        raise ValueError("invalid_frozen_repeat_count")
    selected = case_ids if case_ids is not None else [c["case_id"] for c in bundle["cases"][:3]]
    known = {c["case_id"] for c in bundle["cases"]}
    if not selected or len(set(selected)) != len(selected) or not set(selected) <= known:
        raise ValueError("invalid_selected_case_ids")
    source = out / "input"
    source.mkdir()
    config["bundle"]["path"] = str(source / "bundle.json")
    save(source / "config.json", config)
    save(source / "bundle.json", bundle)
    experiment = {
        "schema_version": 3,
        "experiment_id": "current-qwen-one-thread-three-case-resource-overhead"
        + ("-fixed32" if fixed_output else ""),
        "name": "Current-source three-case sampler ABBA and matched targets",
        "definition_versions": {
            "measurement": "phase2.v1",
            "scoring": "phase2.v1",
            "comparison": "phase2.v1",
        },
        "execution": {
            "concurrency": 1,
            "automatic_retries": 0,
            "order": "fixed",
            "seed": None,
            "service_transition": "operator_verified",
        },
        "comparison": comparison or {"mode": "model", "factor": None},
        "budget": {
            "max_requests": 100,
            "max_wall_seconds": 1800,
            "min_disk_bytes": 5 * 1024**3,
            "min_available_memory_bytes": 8 * 1024**3,
        },
        "performance_environment": {
            "max_external_cpu_percent": 25,
            "max_external_interval_seconds": 30,
        },
        "resource_comparison": {
            "min_coverage_ratio": 0.2,
            "max_window_offset_difference": 0.5,
            "max_sample_gap_intervals": 2,
            "min_baseline_samples": 2,
        },
        "safety": {
            "interval_seconds": 1,
            "max_temperature_celsius": None,
            "require_temperature": False,
            "max_external_cpu_percent": 25,
            "check_environment": True,
        },
        "workloads": [
            {
                "workload_id": "approved-short-subset",
                "purpose": "performance" if fixed_output else "quality",
                "config": {"path": "config.json", "sha256": sha256_file(source / "config.json")},
                "bundle": {"path": "bundle.json", "sha256": sha256_file(source / "bundle.json")},
                "protocol": {
                    "kind": "fixed",
                    "case_ids": selected,
                },
                "repeats": repeats,
                "timeout_seconds": config["execution"]["timeout_seconds"],
                "overhead_budget_seconds": 300,
                "input_target_tokens": None,
                "output_budget_tokens": config["generation"]["max_tokens"],
                **({"output_mode": "strict_fixed_length"} if fixed_output else {}),
            }
        ],
    }
    save(source / "experiment.json", experiment)
    write_plan(source / "experiment.json", out / "frozen")
    return read_frozen_plan(out / "frozen/plan.json")


async def measure(
    out, plan, loaded, deadline, *, first_events=False, engine_rates=False, external_pair=False
):
    trial = plan["trials"][0]
    inputs = loaded[trial["workload_id"]]
    announce("abba_started", protocol="v3", tolerance_ratio=0.05)
    result = await run_overhead(
        plan,
        trial["trial_id"],
        inputs,
        out / "abba",
        tolerance_ratio=0.05,
        max_wall_seconds=min(360, deadline - time.monotonic()),
        boundary_observer=True,
        first_event_tolerance_ratio=0.05 if first_events else None,
        engine_rate_tolerance_ratio=0.05 if engine_rates else None,
    )
    announce("abba_finished", passed=result["passed"], reasons=result["reasons"])
    if not result["passed"]:
        save(
            out / "verification.json",
            {"qualified": False, "stage": "abba", "result": result, "targets_started": 0},
        )
        return 3
    if first_events and not all(
        result.get("first_event_assessments", {}).get(code, {}).get("passed")
        for code in ("L01", "L02")
    ):
        save(
            out / "verification.json",
            {
                "qualified": False,
                "stage": "first_event_overhead",
                "result": result,
                "targets_started": 0,
            },
        )
        announce("first_event_overhead_refused", assessments=result.get("first_event_assessments"))
        return 3
    if engine_rates and not all(
        result.get("engine_rate_assessments", {}).get(code, {}).get("passed")
        for code in ("L06", "L07")
    ):
        save(
            out / "verification.json",
            {
                "qualified": False,
                "stage": "engine_rate_overhead",
                "result": result,
                "targets_started": 0,
            },
        )
        announce("engine_rate_overhead_refused", assessments=result.get("engine_rate_assessments"))
        return 3
    arms = read_json(out / "abba/trials.json")
    environment = read_overhead(out / "abba", target=out / "abba" / arms[1]["path"])
    save(out / "preflight-environment.json", environment)
    if not environment["environment_binding"]["eligible"]:
        save(
            out / "verification.json",
            {
                "qualified": False,
                "stage": "preflight_environment",
                "reasons": environment["environment_binding"]["reasons"],
                "targets_started": 0,
            },
        )
        announce(
            "preflight_environment_refused", reasons=environment["environment_binding"]["reasons"]
        )
        return 3
    if inputs.bundle.to_dict()["bundle_id"] == "current-sampler-fixed32-diagnostic":
        save(
            out / "verification.json",
            {
                "qualified": False,
                "sampler_preflight_passed": True,
                "stage": "engineering_preflight_passed",
                "result": result,
                "targets_started": 0,
                "scope": "unreviewed_fixed32_diagnostic_no_formal_quality_or_pair_acceptance",
            },
        )
        announce("engineering_preflight_finished", sampler_passed=True, formal_qualified=False)
        return 0
    targets = []
    for index in range(2):
        if time.monotonic() >= deadline:
            save(
                out / "verification.json",
                {
                    "qualified": False,
                    "stage": "overall_wall_budget",
                    "targets_started": len(targets),
                },
            )
            return 3
        announce("target_started", index=index + 1)
        code, data, path = await run_trial(
            plan,
            plan["trials"][index]["trial_id"],
            inputs,
            out / f"target-{index + 1}",
            wall_budget_seconds=min(300, deadline - time.monotonic()),
            diagnostic=inputs.bundle.to_dict()["bundle_id"] == "current-sampler-fixed32-diagnostic",
        )
        targets.append(path)
        announce(
            "target_finished",
            index=index + 1,
            exit_code=code,
            stop_reason=data["summary"]["stop_reason"],
        )
        if code or data["summary"]["completeness"] != "complete":
            save(
                out / "verification.json",
                {
                    "qualified": False,
                    "stage": "target",
                    "target": str(path),
                    "exit_code": code,
                    "summary": data["summary"],
                },
            )
            return 3
    if external_pair:
        save(
            out / "verification.json",
            {
                "qualified": False,
                "stage": "targets_completed_external_pair_pending",
                "targets": [str(p) for p in targets],
                "scope": "independent_preflight_and_targets_pair_comparison_required",
            },
        )
        return 0
    result = build_comparison(*targets, left_overhead=out / "abba", right_overhead=out / "abba")
    comparison = out / "comparison"
    comparison.mkdir()
    save(comparison / "comparison.json", result)
    verified = verify_comparison(comparison)
    qualified = result["eligibility"]["performance"]
    if first_events:
        qualified = qualified and {"L01", "L02"} <= {
            r["metric_id"] for r in result["performance_analysis"]["differences"] if r["eligible"]
        }
    if engine_rates:
        qualified = qualified and {"L06", "L07"} <= {
            r["metric_id"] for r in result["performance_analysis"]["differences"] if r["eligible"]
        }
    save(
        out / "verification.json",
        {
            "qualified": qualified,
            "stage": "comparison",
            "roundtrip": verified,
            "targets": [str(p) for p in targets],
            "performance": result["performance_analysis"],
            "qualified_metric_ids": sorted(
                {
                    r["metric_id"]
                    for r in result["performance_analysis"]["differences"]
                    if r["eligible"]
                }
            ),
            "scope": "same_recipe_three_case_repeats_not_model_ranking_or_phase2_completion",
        },
    )
    announce(
        "comparison_finished",
        qualified=qualified,
        blockers=result["performance_analysis"]["blockers"],
    )
    return 0 if qualified else 3


async def interruptible_measure(*args, **kwargs):
    import signal

    loop, task = asyncio.get_running_loop(), asyncio.current_task()
    previous = signal.getsignal(signal.SIGTERM)
    loop.add_signal_handler(signal.SIGTERM, task.cancel)
    try:
        return await measure(*args, **kwargs)
    finally:
        loop.remove_signal_handler(signal.SIGTERM)
        signal.signal(signal.SIGTERM, previous)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--input", required=True, type=Path, help="frozen config.json/bundle.json directory"
    )
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("--interval-ms", type=int, choices=(500, 1000, 2000), default=1000)
    parser.add_argument(
        "--first-events",
        action="store_true",
        help="freeze separate 5 percent L01/L02 gates; stop on either refusal",
    )
    parser.add_argument(
        "--engine-rates",
        action="store_true",
        help="freeze separate 5 percent L06/L07 gates; stop on either refusal",
    )
    parser.add_argument(
        "--fixed-output",
        action="store_true",
        help="freeze strict 32-token performance workload; no quality scores",
    )
    args = parser.parse_args()
    out = args.out.resolve()
    out.mkdir(parents=True, exist_ok=False)
    config, bundle = prepare(
        out, source=args.input, interval_ms=args.interval_ms, fixed_output=args.fixed_output
    )
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    startup = config["engine"]["startup_args"]
    startup[startup.index("--port") + 1] = str(port)
    command = [config["engine"]["binary_path"], *startup]
    save(out / "service-command.json", command)
    endpoint = f"http://127.0.0.1:{port}"
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    deadline = time.monotonic() + 600
    with (out / "server.log").open("wb") as log:
        process = subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT)
        announce("owned_service_started", pid=process.pid)
        try:
            ready_deadline = time.monotonic() + 120
            while time.monotonic() < ready_deadline:
                if process.poll() is not None:
                    raise RuntimeError("owned_service_exited")
                try:
                    with opener.open(endpoint + "/health", timeout=2):
                        break
                except urllib.error.URLError, TimeoutError:
                    time.sleep(0.25)
            else:
                raise TimeoutError("service_readiness_timeout")
            config["endpoint"].update(
                url=endpoint,
                server_pid=process.pid,
                process_start_ticks=process_start_ticks(process.pid),
            )
            plan, loaded = freeze(out, config, bundle, fixed_output=args.fixed_output)
            code = asyncio.run(
                interruptible_measure(
                    out,
                    plan,
                    loaded,
                    deadline,
                    first_events=args.first_events,
                    engine_rates=args.engine_rates,
                )
            )
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
            save(
                out / "lifecycle.json",
                {
                    "owned_pid": process.pid,
                    "stopped": process.poll() is not None,
                    "exit_code": process.returncode,
                },
            )
            announce("owned_service_stopped", pid=process.pid, exit_code=process.returncode)
    raise SystemExit(code)


if __name__ == "__main__":
    main()
