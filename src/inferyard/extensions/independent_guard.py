"""A common read-only native guardian in a separate OS process for both arms."""

import hashlib
import multiprocessing as mp
import os
import shutil
import time
from pathlib import Path

from inferyard.analysis.environment_identity import fields
from inferyard.evidence.storage import EvidenceError, json_bytes, read_jsonl
from inferyard.platforms.external_cpu import native_macos, read_boot_id
from inferyard.platforms.identity import (
    PreflightError,
    check_resources,
    environment_snapshot,
    process_start_ticks,
)
from inferyard.platforms.platform_io import open_nofollow
from inferyard.runtime.safety import SafetyGuard

POLICY = {
    "interval_seconds": 0.5,
    "max_temperature_celsius": 90,
    "require_temperature": True,
    "max_external_cpu_percent": 20,
    "check_environment": True,
}


def _capture(path, config, policy, shutdown, unsafe, ready):
    guard = SafetyGuard(policy, config, environment_snapshot)
    fd = open_nofollow(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL)
    with os.fdopen(fd, "wb") as stream:
        final = False
        while True:
            started = time.monotonic_ns()
            safe, reason = True, None
            try:
                guard.check(periodic=True)
                if "output" in config:
                    environment = (guard.last or {}).get("environment", {}) or {}
                    ancestor = Path(config["output"]["root"])
                    while not ancestor.exists():
                        ancestor = ancestor.parent
                    check_resources(
                        config,
                        environment.get("mem_available_bytes"),
                        shutil.disk_usage(ancestor).free,
                    )
            except PreflightError as exc:
                safe, reason = False, str(exc)
            except Exception:
                safe, reason = False, "independent_guard_source_unavailable"
            env = (guard.last or {}).get("environment", {}) or {}
            identity = {
                k: env.get(k)
                for k in (
                    fields([env])
                    if env.get("platform") == "Darwin"
                    else ("platform", "kernel", "cpu_model", "ac_online", "governor", "epp")
                )
            }
            row = {
                "monotonic_ns": started,
                "safe": safe,
                "reason": reason,
                "environment_identity": identity,
                "observation": guard.last,
                "read_finished_ns": time.monotonic_ns(),
            }
            stream.write(json_bytes(row))
            stream.flush()
            os.fsync(stream.fileno())
            if not safe:
                unsafe.set()
            ready.set()
            if final:
                break
            # One final sample after the arm's close brackets the complete lifecycle.
            final = shutdown.wait(policy["interval_seconds"])
            if final:
                remaining = policy["interval_seconds"] - (time.monotonic_ns() - started) / 1e9
                if remaining > 0:
                    time.sleep(remaining)


class IndependentGuard:
    def __init__(self, path, config, *, policy=None):
        self.path, self.config, self.policy = Path(path), config, dict(policy or POLICY)
        self.clock_id = read_boot_id() + ":CLOCK_MONOTONIC"
        context = mp.get_context("spawn" if native_macos() else "fork")
        self.shutdown, self.unsafe, self.ready = (context.Event() for _ in range(3))
        self.process = context.Process(
            target=_capture,
            args=(str(self.path), config, self.policy, self.shutdown, self.unsafe, self.ready),
        )

    def start(self):
        self.process.start()
        if not self.ready.wait(5) or not self.process.is_alive():
            self.close()
            raise EvidenceError("independent_guard_did_not_start")
        self.start_ticks = process_start_ticks(self.process.pid)
        return self

    def stopped(self):
        return self.unsafe.is_set() or not self.process.is_alive()

    def close(self):
        if self.process.pid is None:
            return
        self.shutdown.set()
        self.process.join(5)
        if self.process.is_alive():
            self.process.terminate()
            self.process.join(5)
            raise EvidenceError("independent_guard_did_not_stop_cleanly")

    def evidence(self):
        rows, limitations = read_jsonl(self.path)
        if limitations or self.process.exitcode != 0:
            raise EvidenceError("independent_guard_evidence_incomplete")
        return {
            "pid": self.process.pid,
            "process_start_ticks": self.start_ticks,
            "clock_id": self.clock_id,
            "policy_sha256": hashlib.sha256(json_bytes(self.policy)).hexdigest(),
            "policy": self.policy,
            "samples": rows,
        }
