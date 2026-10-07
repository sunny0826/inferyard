"""Read-only /proc validation against an owned CPU fixture, not a model benchmark."""

import argparse
import json
import subprocess
import sys
import time
from datetime import UTC, datetime
from pathlib import Path

from inferyard.analysis.resource_observations import build_resource_observations
from inferyard.contracts.validation import validate_document
from inferyard.evidence.storage import sha256_file
from inferyard.platforms.identity import process_start_ticks
from inferyard.platforms.resources_linux import ResourceSampler
from inferyard.provenance import tool_source_hash

WORKER = """
import sys, time
print('ready', flush=True)
sys.stdin.readline()
deadline = time.monotonic() + 0.6
n = 1
while time.monotonic() < deadline:
    n = (n * 17 + 3) % 10000019
print('done', flush=True)
sys.stdin.readline()
"""


class Recorder:
    def __init__(self):
        self.samples, self.snapshots, self.schedule = [], {}, []

    def snapshot(self, name, value):
        self.snapshots[name] = value

    def sample(self, value):
        sample = {
            **value,
            "schema_version": 3,
            "experiment_id": "collector-fixture",
            "trial_id": "collector-fixture",
            "run_id": "collector-fixture",
            "clock_id": "collector-fixture-clock",
            "seq": len(self.samples) + 1,
            "utc": datetime.now(UTC).isoformat(),
        }
        validate_document("sample", sample)
        self.samples.append(sample)

    def observation(self, name, value):
        self.schedule.append({"log": name, **value})


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=False)
    process = subprocess.Popen(
        [sys.executable, "-c", WORKER], text=True, stdin=subprocess.PIPE, stdout=subprocess.PIPE
    )
    try:
        if process.stdout.readline().strip() != "ready":
            raise RuntimeError("fixture worker did not start")
        endpoint = {
            "server_pid": process.pid,
            "process_start_ticks": process_start_ticks(process.pid),
        }
        config = {"endpoint": endpoint, "telemetry": {"interval_ms": 20}}
        record = Recorder()
        sampler = ResourceSampler(record, config)
        sampler.set_phase("baseline")
        for _ in range(2):
            for sample in sampler.collect(process.pid, endpoint["process_start_ticks"]):
                record.sample(sample)
            time.sleep(0.02)
        row = {
            "case_id": "cpu-fixture",
            "category": "performance",
            "request_id": "cpu-fixture",
            "execution_state": "completed",
            "clock_id": "collector-fixture-clock",
            "t_send_ns": time.monotonic_ns(),
        }
        sampler.set_phase("formal", row["request_id"])
        sampler.boundary("request_start")
        process.stdin.write("go\n")
        process.stdin.flush()
        until = time.monotonic() + 0.8
        while time.monotonic() < until:
            for sample in sampler.collect(process.pid, endpoint["process_start_ticks"]):
                record.sample(sample)
            time.sleep(0.02)
        row["t_terminal_ns"] = time.monotonic_ns()
        sampler.boundary("request_end")
        assert process.stdout.readline().strip() == "done"
        payload = {
            "kind": "owned_cpu_process_collector_fixture_not_model_acceptance",
            "tool_source_sha256": tool_source_hash(),
            "config": config,
            "requests": [row],
            "samples": record.samples,
            "snapshots": record.snapshots,
            "schedule": record.schedule,
        }
        raw = args.out / "raw.json"
        raw.write_text(json.dumps(payload, indent=2) + "\n")
        run = {
            "run_id": "collector-fixture",
            "trial_id": "collector-fixture",
            "definition_versions": dict(
                measurement="phase2.v1", scoring="phase2.v1", comparison="phase2.v1"
            ),
        }
        reduced, metrics = build_resource_observations(
            run,
            "collector-fixture",
            [row],
            record.samples,
            config,
            [{"path": "raw.json", "sha256": sha256_file(raw)}],
            complete=False,
        )
        cpu = reduced["requests"][0]["metrics"]["C04"]
        passed = cpu["value"] is not None and cpu["value"] > 0 and 0 < cpu["coverage"] < 1
        result = {
            "passed": passed,
            "kind": payload["kind"],
            "tool_source_sha256": tool_source_hash(),
            "observed_cpu_seconds": cpu["value"],
            "coverage": cpu["coverage"],
            "sample_count": cpu["sample_count"],
            "summary": reduced,
            "metrics": metrics,
        }
        (args.out / "verification.json").write_text(json.dumps(result, indent=2) + "\n")
        print(
            json.dumps(
                {
                    key: result[key]
                    for key in ("passed", "observed_cpu_seconds", "coverage", "sample_count")
                }
            )
        )
        if not passed:
            raise SystemExit(1)
    finally:
        if process.poll() is None:
            try:
                process.stdin.write("exit\n")
                process.stdin.flush()
                process.wait(timeout=3)
            except BrokenPipeError, subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        process.stdin.close()
        process.stdout.close()


if __name__ == "__main__":
    main()
