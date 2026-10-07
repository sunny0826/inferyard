"""Record and stop an owned Linux build process group; never retry or promote outputs.

The caller must hold the benchmark host lock, verify inputs, and provide a
fail-closed temperature check. This helper does not authorize a model build.
"""

import json
import math
import os
import signal
import subprocess
import time
from datetime import UTC, datetime
from pathlib import Path

from inferyard.platforms.sensors_linux import LinuxSensors


class BuildSafetyStop(RuntimeError):
    def __init__(self, reason, evidence):
        super().__init__(reason)
        self.evidence = evidence


class BuildTemperatureCheck:
    """Require every discovered thermal source to remain readable and below limit."""

    def __init__(self, threshold, *, sensors=None):
        if isinstance(threshold, bool) or not math.isfinite(threshold):
            raise ValueError("invalid_temperature_limit")
        self.threshold = threshold
        self.sensors = sensors if sensors is not None else LinuxSensors()
        self.sensors.sources = [
            source for source in self.sensors.sources if source["metric_name"] == "temperature"
        ]

    def __call__(self):
        rows = self.sensors.collect("offline_model_build", None)
        evidence = {"temperature_samples": rows, "threshold_celsius": self.threshold}
        if not rows or any(
            row["value"] is None
            or not math.isfinite(row["value"])
            or row.get("missing_reason") is not None
            for row in rows
        ):
            raise BuildSafetyStop("build_temperature_source_unavailable", evidence)
        if any(row["value"] >= self.threshold for row in rows):
            raise BuildSafetyStop("build_temperature_threshold_reached", evidence)
        return evidence


def group_members(pgid):
    """Return live group members and RSS; exclude zombies awaiting their parent."""
    rows = []
    for path in Path("/proc").glob("[0-9]*/stat"):
        try:
            raw = path.read_text()
            fields = raw[raw.rfind(")") + 2 :].split()
            if int(fields[2]) == pgid and fields[0] != "Z":
                rows.append(
                    {
                        "pid": int(path.parent.name),
                        "rss_bytes": int(fields[21]) * os.sysconf("SC_PAGE_SIZE"),
                    }
                )
        except FileNotFoundError:
            continue
    return rows


def stop_group(process, grace_seconds=2):
    """Clean descendants even when the group leader has already exited."""
    for sig in (signal.SIGTERM, signal.SIGKILL):
        try:
            os.killpg(process.pid, sig)
        except ProcessLookupError:
            break
        deadline = time.monotonic() + grace_seconds
        while time.monotonic() < deadline:
            process.poll()
            if not group_members(process.pid):
                break
            time.sleep(0.02)
        if not group_members(process.pid):
            break
    process.wait(timeout=5)
    return group_members(process.pid)


def run_build(argv, *, environment, out, wall_seconds, rss_bytes, interval, check, pacing=None):
    """Run once. Persist terminal evidence on success, stop, or interruption.

    check() must return JSON-serializable sensor evidence or raise on unavailable
    or unsafe conditions. It is checked before spawn and at sampled intervals.
    Resource checks are sampled, not hard kernel-enforced limits.
    """
    if os.name != "posix":
        raise RuntimeError("linux_build_supervisor_required")
    if (
        not argv
        or not all(isinstance(v, str) for v in argv)
        or any(not math.isfinite(v) or v <= 0 for v in (wall_seconds, rss_bytes, interval))
    ):
        raise ValueError("invalid_build_limits_or_command")
    if pacing is not None and (
        not isinstance(pacing, dict)
        or set(pacing) != {"active_seconds", "rest_seconds"}
        or any(
            type(v) not in (int, float) or not math.isfinite(v) or v <= 0 for v in pacing.values()
        )
        or sum(pacing.values()) > interval
    ):
        raise ValueError("invalid_build_pacing")
    out = Path(out)
    out.mkdir(parents=True, exist_ok=False)
    process = None
    started = time.monotonic()
    result = {
        "argv": argv,
        "started_at": datetime.now(UTC).isoformat(),
        "state": "preflight",
        "automatic_retry": False,
        "requested_pacing": pacing,
        "owned_pid": None,
        "exit_code": None,
        "peak_sampled_rss_bytes": 0,
    }
    previous = {}

    def interrupt(signum, frame):
        raise InterruptedError(f"build_interrupted_signal_{signum}")

    try:
        for sig in (signal.SIGTERM, signal.SIGINT):
            previous[sig] = signal.signal(sig, interrupt)
        with (out / "samples.jsonl").open("x") as samples:
            initial = check()
            samples.write(json.dumps({"elapsed_seconds": 0, "safety": initial}) + "\n")
            samples.flush()
            with (out / "process.log").open("xb") as log:
                process = subprocess.Popen(
                    argv,
                    env=environment,
                    stdout=log,
                    stderr=subprocess.STDOUT,
                    start_new_session=True,
                )
                result["owned_pid"] = process.pid
                result["state"] = "running"
                while True:
                    elapsed = time.monotonic() - started
                    if elapsed >= wall_seconds:
                        raise TimeoutError("build_wall_budget_reached")
                    safety = check()
                    members = group_members(process.pid)
                    total = sum(row["rss_bytes"] for row in members)
                    result["peak_sampled_rss_bytes"] = max(result["peak_sampled_rss_bytes"], total)
                    samples.write(
                        json.dumps(
                            {"elapsed_seconds": elapsed, "members": members, "safety": safety}
                        )
                        + "\n"
                    )
                    samples.flush()
                    if total > rss_bytes:
                        raise RuntimeError("build_sampled_rss_budget_reached")
                    code = process.poll()
                    if code is not None:
                        if group_members(process.pid):
                            raise RuntimeError("build_descendants_remain_after_leader_exit")
                        result["state"] = "succeeded" if code == 0 else "failed"
                        break
                    if pacing is None:
                        time.sleep(min(interval, max(0, wall_seconds - elapsed)))
                    else:
                        time.sleep(min(pacing["active_seconds"], max(0, wall_seconds - elapsed)))
                        if process.poll() is not None:
                            continue
                        os.killpg(process.pid, signal.SIGSTOP)
                        samples.write(
                            json.dumps(
                                {
                                    "control": "SIGSTOP",
                                    "elapsed_seconds": time.monotonic() - started,
                                }
                            )
                            + "\n"
                        )
                        samples.flush()
                        remaining = wall_seconds - (time.monotonic() - started)
                        time.sleep(min(pacing["rest_seconds"], max(0, remaining)))
                        if time.monotonic() - started >= wall_seconds:
                            raise TimeoutError("build_wall_budget_reached")
                        safety = check()
                        samples.write(
                            json.dumps(
                                {
                                    "control": "SIGCONT",
                                    "elapsed_seconds": time.monotonic() - started,
                                    "safety": safety,
                                }
                            )
                            + "\n"
                        )
                        samples.flush()
                        os.killpg(process.pid, signal.SIGCONT)
    except BaseException as exc:
        result["state"] = "stopped"
        result["error"] = {"type": type(exc).__name__, "message": str(exc)}
        if isinstance(exc, BuildSafetyStop):
            result["stop_evidence"] = exc.evidence
        raise
    finally:
        # Suppress a second termination signal while owned descendants are cleaned.
        for sig in previous:
            signal.signal(sig, signal.SIG_IGN)
        try:
            if process is not None:
                try:
                    remaining = stop_group(process)
                    result["remaining_members"] = remaining
                    if remaining:
                        result["state"] = "cleanup_failed"
                except BaseException as exc:
                    result["state"] = "cleanup_failed"
                    result["cleanup_error"] = str(exc)
                result["exit_code"] = process.poll()
            result["finished_at"] = datetime.now(UTC).isoformat()
            result["elapsed_seconds"] = time.monotonic() - started
            (out / "terminal.json").write_text(json.dumps(result, indent=2) + "\n")
        finally:
            for sig, handler in previous.items():
                signal.signal(sig, handler)
    return result
