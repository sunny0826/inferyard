"""Bounded real natural-output ABBA and matched performance/resource trials.

The audited corpus is unchanged. Selection is frozen before any requests; no
retry or tolerance adjustment is made after seeing the measurement.
"""

import argparse
import asyncio
import socket
import subprocess
import time
import urllib.error
import urllib.request
from pathlib import Path

from verify_current_sampler_overhead import announce, freeze, interruptible_measure, save

from inferyard.evidence.storage import read_json, sha256_file
from inferyard.platforms.identity import process_start_ticks
from inferyard.provenance import tool_source_hash
from inferyard.runtime.runner import require_review

CASES = ["extraction-11", "extraction-18", "extraction-20"]


def prepare(out, *, source, threads=1):
    if threads not in (1, 2) or type(threads) is not int:
        raise ValueError("unsupported_bounded_thread_recipe")
    config = read_json(source / "config.json")
    bundle = read_json(source / "bundle.json")
    require_review(bundle)
    config["generation"]["max_tokens"] = 128
    config["execution"]["timeout_seconds"] = 60
    config["telemetry"]["interval_ms"] = 500
    for key, flag, value in (("threads", "-t", threads), ("threads_batch", "-tb", 1)):
        config["conditions"][key] = value
        args = config["engine"]["startup_args"]
        args[args.index(flag) + 1] = str(value)
    save(
        out / "acceptance-policy.json",
        {
            "kind": "approved_natural_output_resource_performance_trial.v1",
            "case_ids": CASES,
            "selection_basis": "three three-field JSON extraction cases by corpus structure",
            "tool_source_sha256": tool_source_hash(),
            "driver_sha256": sha256_file(Path(__file__)),
            "scope": "same-model same-recipe repeat comparison; selected cases only",
            "overhead_tolerance_ratio": 0.05,
            "required_resources": ["C01", "C02", "C04"],
            "required_performance": ["L03"],
            "output_mode": "natural_stop",
            "threads": threads,
            "threads_batch": 1,
            "max_tokens": 128,
            "max_wall_seconds": 600,
            "automatic_retries": 0,
            "temperature_and_epp": "observations_only_per_user_policy",
            "total_shared_safety_and_boundary_perturbation_qualified": False,
        },
    )
    return config, bundle


def qualify_resources(out):
    path = out / "comparison/comparison.json"
    if not path.is_file():
        return False
    comparison = read_json(path)
    rows = comparison["performance_analysis"]["differences"]
    policy = read_json(out / "acceptance-policy.json")
    required = policy["required_resources"] + policy["required_performance"]
    allowed = [r for r in rows if r["eligible"]]
    missing = [
        {"metric_id": code, "case_id": case}
        for code in required
        for case in policy["case_ids"]
        if not any(r["metric_id"] == code and r["case_id"] == case for r in allowed)
    ]
    save(
        out / "resource-verification.json",
        {
            "qualified": not missing,
            "missing_required_pairs": missing,
            "allowed_metric_ids": sorted({r["metric_id"] for r in allowed}),
            "allowed_rows": len(allowed),
            "source_comparison_sha256": sha256_file(path),
            "scope": "matched observed request windows only",
        },
    )
    return not missing


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--input", required=True, type=Path, help="frozen config.json/bundle.json directory"
    )
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("--check", action="store_true", help="freeze only; zero model requests")
    args = parser.parse_args()
    out = args.out.resolve()
    out.mkdir(parents=True, exist_ok=False)
    config, bundle = prepare(out, source=args.input)
    if args.check:
        plan, _ = freeze(out, config, bundle, case_ids=CASES)
        announce("dry_run_finished", plan_sha256=plan["plan_sha256"], model_requests=0)
        return 0
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
            plan, loaded = freeze(out, config, bundle, case_ids=CASES)
            announce("recipe_frozen", plan_sha256=plan["plan_sha256"], case_ids=CASES)
            code = asyncio.run(interruptible_measure(out, plan, loaded, deadline))
            if code == 0:
                resource_ok = qualify_resources(out)
                announce("resource_comparison_finished", qualified=resource_ok)
                code = 0 if resource_ok else 3
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
    return code


if __name__ == "__main__":
    raise SystemExit(main())
