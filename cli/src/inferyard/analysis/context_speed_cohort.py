"""Stable, observed measurement identities; resource readings are not identities."""

import hashlib

from inferyard.analysis.engine_timing import DEFINITION
from inferyard.evidence.storage import json_bytes


def _known(value):
    return value is not None and value not in ("", "unknown", "not_observed", "unavailable")


def cohort(config, environment, token):
    hardware = {
        k: environment.get(k)
        for k in (
            "platform",
            "architecture",
            "kernel",
            "cpu_model",
            "logical_cpus",
            "memory_total_bytes",
        )
    }
    if not all(_known(v) for v in hardware.values()):
        return None
    gpu = environment.get("gpu", {})
    backend = config["engine"]["backend"]
    if backend == "cpu":
        hardware["gpu"] = {"backend": "not_used"}
    elif gpu.get("backend") != backend or not gpu.get("devices"):
        return None
    else:
        keys = (
            ("index", "name", "driver_version", "memory_total_bytes")
            if backend == "cuda"
            else ("name", "metal_supported")
        )
        devices = [{k: device.get(k) for k in keys} for device in gpu["devices"]]
        if not all(_known(v) for device in devices for v in device.values()):
            return None
        hardware["gpu"] = {"backend": backend, "devices": devices}
    return hashlib.sha256(
        json_bytes(
            {
                "definition": DEFINITION,
                "hardware": hardware,
                "device": config["device"],
                "model": {k: config["model"][k] for k in ("sha256", "template_sha256")},
                "engine": {
                    k: config["engine"][k]
                    for k in ("adapter", "release", "binary_sha256", "backend", "startup_args")
                },
                "conditions": config["conditions"],
                "generation": config["generation"],
                "input": token,
                "output_mode": "strict_fixed_length",
            }
        )
    ).hexdigest()
