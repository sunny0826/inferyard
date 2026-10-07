"""Read-only, identity-bound thermal-zone and CPUFreq sensor discovery.

Thermal ABI: driver-api/thermal/sysfs-api.html (millidegrees Celsius).
CPUFreq ABI: admin-guide/pm/cpufreq.html (kHz). scaling_cur_freq is
requested/driver-reported, never asserted to be effective execution frequency.
Unknown hwmon drivers are deliberately not assigned a temperature unit.
"""

import time
from pathlib import Path


class SensorError(ValueError):
    pass


class LinuxSensors:
    def __init__(self, root=Path("/sys"), *, clock=time.monotonic_ns, limit=256):
        if type(limit) is not int or limit < 1:
            raise ValueError("positive sensor limit required")
        self.root = Path(root).resolve()
        self.clock = clock
        self.sources = []
        self.issues = []
        self.lost = set()
        specs = (
            (
                "class/thermal/thermal_zone*/temp",
                "temperature",
                "celsius",
                0.001,
                "thermal_zone_reported",
                ("type",),
            ),
            (
                "devices/system/cpu/cpufreq/policy*/cpuinfo_cur_freq",
                "frequency",
                "Hz",
                1000,
                "hardware_reported",
                ("scaling_driver", "related_cpus"),
            ),
            (
                "devices/system/cpu/cpufreq/policy*/scaling_cur_freq",
                "frequency",
                "Hz",
                1000,
                "requested_or_driver_reported",
                ("scaling_driver", "related_cpus"),
            ),
        )
        for pattern, metric, unit, scale, semantics, identity_fields in specs:
            for path in self.root.glob(pattern):
                if len(self.sources) >= limit:
                    self.issues.append({"reason": "discovery_limit_reached", "pattern": pattern})
                    break
                source = {
                    "source": str(path.relative_to(self.root)),
                    "metric_name": metric,
                    "unit": unit,
                    "scale": scale,
                    "semantics": semantics,
                    "identity_fields": list(identity_fields),
                    "identity": None,
                    "missing_reason": None,
                }
                try:
                    source["identity"] = self.identity(path, identity_fields)
                except (OSError, ValueError) as exc:
                    source["missing_reason"] = self.reason(exc)
                self.sources.append(source)
        self.sources.sort(key=lambda source: source["source"])

    def safe_path(self, path):
        resolved = path.resolve(strict=True)
        if not resolved.is_relative_to(self.root):
            raise SensorError("source_outside_sysfs")
        return resolved

    def read(self, path):
        # Bounded reads also reject malformed or unexpectedly large attributes.
        with self.safe_path(path).open() as stream:
            value = stream.read(4097).strip()
        if not value or len(value) > 4096:
            raise SensorError("invalid_sensor_attribute")
        return value

    def identity(self, path, fields):
        resolved = self.safe_path(path)
        stat = resolved.stat()
        return {
            "path": str(resolved.relative_to(self.root)),
            "inode": stat.st_ino,
            "device": stat.st_dev,
            "attributes": {field: self.read(path.parent / field) for field in fields},
        }

    @staticmethod
    def reason(exc):
        if isinstance(exc, PermissionError):
            return "permission_denied"
        if isinstance(exc, SensorError):
            return str(exc)
        if isinstance(exc, OSError):
            return "source_unavailable"
        return "invalid_sensor_value"

    def metadata(self):
        return {
            "sources": self.sources,
            "discovery_issues": self.issues,
            "limitations": [
                "no_throttling_inference",
                "no_cross_sensor_aggregation",
                "hwmon_driver_units_not_verified",
            ],
            "missing_metrics": [
                metric
                for metric in ("temperature", "frequency")
                if not any(s["metric_name"] == metric for s in self.sources)
            ],
        }

    def collect(self, phase, request_id):
        samples = []
        for source in self.sources:
            started = self.clock()
            value = raw = None
            reason = source["missing_reason"]
            path = self.root / source["source"]
            if not reason:
                try:
                    if source["source"] in self.lost:
                        raise SensorError("source_changed")
                    before = self.identity(path, source["identity_fields"])
                    if before != source["identity"]:
                        raise SensorError("source_changed")
                    raw = int(self.read(path))
                    if source["metric_name"] == "frequency" and raw <= 0:
                        raise SensorError("invalid_sensor_value")
                    if source["metric_name"] == "temperature" and raw < -273150:
                        raise SensorError("invalid_sensor_value")
                    value = raw * source["scale"]
                    if self.identity(path, source["identity_fields"]) != before:
                        raise SensorError("source_changed")
                except (OSError, ValueError, OverflowError) as exc:
                    reason = self.reason(exc)
                    if reason in ("source_changed", "source_unavailable"):
                        self.lost.add(source["source"])
            if reason:
                value = raw = None
            samples.append(
                {
                    "collector": "linux-sensors.v2",
                    "server_pid": None,
                    "process_start_ticks": None,
                    "phase": phase,
                    "request_id": request_id,
                    "metric_name": source["metric_name"],
                    "source": source["source"],
                    "unit": source["unit"],
                    "semantics": source["semantics"],
                    "raw_value": raw,
                    "value": value,
                    "missing_reason": reason,
                    "read_started_ns": started,
                    "read_finished_ns": self.clock(),
                }
            )
        return samples
