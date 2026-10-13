"""Narrow protocol validation for additional externally managed engine-fit engines."""

from __future__ import annotations

import re

COMMON_METRICS = {
    "llama-cpp": ("llamacpp:requests_processing", "llamacpp:requests_deferred"),
    "mlx-lm": (
        "engine_fit:num_requests_running",
        "engine_fit:num_requests_waiting",
        "engine_fit:poisoned",
    ),
}


def _mlx_source_sha256():
    from inferyard.config.engine_fit_assets import mlx_server_sha256

    return mlx_server_sha256()


def common_service(engine, payload, served_model, error):
    """Persist only documented version fields, never the complete props response."""
    if engine == "llama-cpp":
        version = payload.get("build_info")
        # Upstream /props exposes llama_build_info(), formatted b<build>-<commit>.
        pattern = r"b[0-9]{1,12}-(?:[a-fA-F0-9]{7,40}|unknown)"
        source = "/props"
    else:
        if (
            payload.get("engine_fit_protocol") != "mlx-lm.v1"
            or payload.get("engine_fit_source_sha256") != _mlx_source_sha256()
        ):
            raise error("mlx_service_protocol_unverified")
        version = payload.get("version")
        pattern = r"[0-9][A-Za-z0-9.+_-]{0,127}"
        source = "/version"
    if not isinstance(version, str) or not re.fullmatch(pattern, version):
        raise error("engine_version_unverified")
    return {
        "engine": engine,
        "version": version,
        "served_model": served_model,
        "version_source": source,
    }


def observer_service(value, served_model, error):
    expected = {
        "engine": "lmstudio",
        "version": None,
        "served_model": served_model,
        "version_source": "not_exposed",
        "version_missing_reason": "lmstudio_service_version_not_exposed",
    }
    if value != expected:
        raise error("lmstudio_service_unverified")
    return expected


def observer_idle(value, served_model, error):
    if (
        not isinstance(value, dict)
        or set(value) != {"idle", "source", "values"}
        or type(value["idle"]) is not bool
        or value["source"] != "lms:ps"
        or not isinstance(value["values"], list)
        or len(value["values"]) != 2
    ):
        raise error("lmstudio_idle_unverified")
    observed = {}
    for sample in value["values"]:
        if (
            not isinstance(sample, dict)
            or set(sample) != {"metric", "labels", "value"}
            or sample["metric"] not in ("lmstudio:queued", "lmstudio:active")
            or sample["metric"] in observed
            or sample["labels"] != {"instance": served_model}
            or type(sample["value"]) is not int
            or sample["value"] < 0
            or (sample["metric"] == "lmstudio:active" and sample["value"] > 1)
        ):
            raise error("lmstudio_idle_unverified")
        observed[sample["metric"]] = sample["value"]
    idle = all(count == 0 for count in observed.values())
    if value["idle"] is not idle:
        raise error("lmstudio_idle_unverified")
    return {
        "idle": idle,
        "source": "lms:ps",
        "values": [
            {"metric": name, "labels": {"instance": served_model}, "value": count}
            for name, count in sorted(observed.items())
        ],
    }
