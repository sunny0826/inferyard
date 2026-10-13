"""Identity-bound SMC temperature samples; unknown encodings remain missing."""

import math
import struct
import time

from inferyard.platforms.smc_macos import SMC, SMCError


class MacSensors:
    def __init__(self, *, factory=SMC, clock=time.monotonic_ns, limit=128):
        self.clock, self.sources, self.issues, self.lost = clock, [], [], set()
        self.client = None
        try:
            self.client = factory()
            keys = sorted(k for k in self.client.keys() if k.startswith(("Tp", "Tg")))
            if not keys or len(keys) > limit:
                raise SMCError("smc_temperature_inventory_unavailable")
            for key in keys:
                info = self.client.info(key)
                # These namespaces are chip thermal sensors; do not infer a core.
                if info["type"] != "flt " or info["size"] != 4:
                    self.issues.append({"key": key, "reason": "smc_temperature_encoding_unknown"})
                    continue
                self.sources.append(
                    {
                        "source": "AppleSMC:" + key,
                        "metric_name": "temperature",
                        "unit": "celsius",
                        "scale": 1,
                        "semantics": "smc_key_reported",
                        "identity": {"registry_entry_id": self.client.entry_id, "key": key, **info},
                    }
                )
        except (OSError, ValueError, AttributeError) as exc:
            self.sources = []
            self.issues.append(
                {"reason": str(exc) if isinstance(exc, SMCError) else "smc_source_unavailable"}
            )

    def metadata(self):
        return {
            "sources": self.sources,
            "discovery_issues": self.issues,
            "limitations": [
                "private_smc_abi",
                "sensor_key_not_core_identity",
                "no_cross_sensor_aggregation",
                "no_throttling_inference",
                "frequency_energy_not_collected",
            ],
            "missing_metrics": ["frequency", *([] if self.sources else ["temperature"])],
        }

    def collect(self, phase, request_id):
        samples = []
        for source in self.sources:
            started, value, reason = self.clock(), None, None
            key = source["identity"]["key"]
            try:
                if (
                    key in self.lost
                    or self.client.identity() != source["identity"]["registry_entry_id"]
                ):
                    raise SMCError("source_changed")
                info, raw = self.client.read(key)
                if {"registry_entry_id": self.client.entry_id, "key": key, **info} != source[
                    "identity"
                ]:
                    raise SMCError("source_changed")
                value = struct.unpack("<f", raw)[0]
                if not math.isfinite(value) or not -273.15 <= value <= 200:
                    raise SMCError("invalid_sensor_value")
            except (OSError, ValueError, struct.error, AttributeError) as exc:
                value = None
                reason = str(exc) if isinstance(exc, SMCError) else "smc_source_unavailable"
                if reason == "source_changed":
                    self.lost.add(key)
            samples.append(
                {
                    "collector": "macos-smc.v1",
                    "server_pid": None,
                    "process_start_ticks": None,
                    "phase": phase,
                    "request_id": request_id,
                    "metric_name": "temperature",
                    "source": source["source"],
                    "unit": "celsius",
                    "semantics": "smc_key_reported",
                    "raw_value": value,
                    "value": value,
                    "missing_reason": reason,
                    "read_started_ns": started,
                    "read_finished_ns": self.clock(),
                }
            )
        return samples
