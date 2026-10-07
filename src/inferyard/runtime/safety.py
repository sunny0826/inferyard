"""Opt-in frozen runtime safety checks; stopping is not a model failure."""

import asyncio
from pathlib import Path

from inferyard.config.environment_binding import mismatches
from inferyard.platforms.external_cpu import native_macos
from inferyard.platforms.identity import PreflightError
from inferyard.platforms.resources_linux import read_cpu
from inferyard.platforms.sensors_linux import LinuxSensors


class ExternalCpu:
    def __init__(self, pid, ticks, root=Path("/proc")):
        self.pid, self.ticks, self.root = pid, ticks, root
        self.previous = None

    def sample(self):
        fields = (self.root / "stat").read_text().splitlines()[0].split()
        if fields[0] != "cpu" or len(fields) < 9:
            raise PreflightError("external_cpu_source_unavailable")
        values = [int(n) for n in fields[1:9]]
        if min(values) < 0:
            raise PreflightError("external_cpu_source_unavailable")
        process = read_cpu(self.pid, self.ticks, self.root)
        current = (
            sum(values),
            values[3] + values[4],
            process["user_ticks"] + process["system_ticks"],
        )
        previous, self.previous = self.previous, current
        if previous is None:
            return None
        total, idle, service = [a - b for a, b in zip(current, previous, strict=True)]
        if min(total, idle, service) < 0 or idle > total:
            raise PreflightError("external_cpu_counter_changed")
        return max(0, total - idle - service) / total * 100 if total else None


class SafetyGuard:
    def __init__(self, policy, config, environment, *, sensors=None, sys_root=Path("/sys")):
        self.policy, self.config, self.environment = policy, config, environment
        self.sys_root = sys_root
        external_type = ExternalCpu
        if native_macos():
            from inferyard.platforms.external_cpu_macos import ExternalCpu as MacExternalCpu
            from inferyard.platforms.sensors_macos import MacSensors

            external_type = MacExternalCpu
            default_sensors = MacSensors
        else:
            default_sensors = LinuxSensors
        self.sensors = sensors if sensors is not None else default_sensors()
        self.sensors.sources = [
            s for s in self.sensors.sources if s["metric_name"] == "temperature"
        ]
        self.last = None
        self.external = (
            external_type(
                config["endpoint"]["server_pid"], config["endpoint"]["process_start_ticks"]
            )
            if policy["max_external_cpu_percent"] is not None
            else None
        )

    def metadata(self):
        return {
            "policy": self.policy,
            "sensors": self.sensors.metadata(),
            "scope": "temperature_environment_and_host_busy_cpu_minus_service_cpu",
        }

    def check(self, *, periodic=False):
        rows = self.sensors.collect("safety", None)
        values = [r["value"] for r in rows if r["value"] is not None]
        self.last = {
            "temperature_samples": rows,
            "environment": None,
            "external_cpu_percent": None,
        }
        environment = self.environment() if self.policy["check_environment"] else {}
        self.last["environment"] = environment
        external = None
        if self.external and (periodic or self.external.previous is None):
            try:
                external = self.external.sample()
            except (OSError, ValueError) as exc:
                raise PreflightError("external_cpu_source_unavailable") from exc
        self.last["external_cpu_percent"] = external
        if "intel_pstate_no_turbo" in self.policy:
            source = "devices/system/cpu/intel_pstate/no_turbo"
            observed = None
            try:
                with (self.sys_root / source).open() as stream:
                    raw = stream.read(16).strip()
                if raw in ("0", "1"):
                    observed = int(raw)
            except OSError:
                pass
            self.last["intel_pstate_no_turbo"] = {"source": source, "value": observed}
            if observed is None:
                raise PreflightError("intel_pstate_safety_source_unavailable")
            if observed != self.policy["intel_pstate_no_turbo"]:
                raise PreflightError("intel_pstate_safety_condition_changed")
        if external is not None and external > self.policy["max_external_cpu_percent"]:
            raise PreflightError("external_cpu_safety_threshold_reached")
        threshold = self.policy["max_temperature_celsius"]
        if threshold is not None and any(value >= threshold for value in values):
            raise PreflightError("temperature_safety_threshold_reached")
        if self.policy["require_temperature"] and (not rows or len(values) != len(rows)):
            raise PreflightError("temperature_safety_source_unavailable")
        if self.policy["check_environment"]:
            if mismatches(self.config["conditions"], environment):
                raise PreflightError("environment_safety_condition_changed")


async def monitor(interval, check, stop):
    """Run in the trial event loop; one stop callback, no automatic retry."""
    while True:
        await asyncio.sleep(interval)
        try:
            check()
        except PreflightError as exc:
            stop(str(exc))
            return
        except Exception:
            stop("runtime_safety_check_failed")
            return
