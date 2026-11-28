"""Pure validation for formal local service observations and queue samples."""

import hashlib
import re
from importlib.resources import files

from inferyard.adapters.engine_fit import FitTransportError, _idle_metrics, parse_json_body
from inferyard.adapters.engine_fit_common import common_service, observer_idle, observer_service
from inferyard.contracts.engine_profiles import LOCAL_FAMILIES

FAMILIES = LOCAL_FAMILIES
VERSION_PATHS = {
    "llama-cpp": "/props",
    "mlx-lm": "/version",
    "vllm": "/version",
    "polystrata": "/v1/status",
}
_STRATA_LOADED_METRIC = re.compile(
    r"^strata:model_loaded(?:\{[^}]*\})?\s+(\S+)(?:\s+\S+)?$"
)


def mlx_sources():
    return {
        key: hashlib.sha256(files("inferyard").joinpath(path).read_bytes()).hexdigest()
        for key, path in (
            ("local_engine_source_sha256", "data/local_engine/mlx_server.py"),
            ("generation_queue_source_sha256", "data/engine_fit/mlx_server.py"),
        )
    }


def models(raw, served_model, *, family=None):
    listing = parse_json_body(raw).get("data")
    if not isinstance(listing, list) or not listing or any(not isinstance(item, dict) for item in listing):
        raise FitTransportError("served_model_unverified")
    if family == "polystrata":
        primary = [item for item in listing if "alias_of" not in item]
        aliases = [item for item in listing if "alias_of" in item]
        if (
            len(primary) != 1
            or primary[0].get("id") != served_model
            or any(
                not isinstance(item.get("id"), str)
                or not item["id"].strip()
                or item.get("alias_of") != served_model
                for item in aliases
            )
        ):
            raise FitTransportError("served_model_unverified")
        return
    if len(listing) != 1 or listing[0].get("id") != served_model:
        raise FitTransportError("served_model_unverified")


def _polystrata_context(payload):
    for candidate in (
        payload.get("max_context"),
        payload.get("cache_max_tokens"),
        (payload.get("context") or {}).get("max_positions")
        if isinstance(payload.get("context"), dict)
        else None,
        (payload.get("context") or {}).get("native")
        if isinstance(payload.get("context"), dict)
        else None,
    ):
        if type(candidate) is int and candidate > 0:
            return candidate
    return None


def _polystrata_service(payload, config, health):
    served = config["engine"]["served_model"]
    expected_ctx = config["conditions"]["context_size"]
    if not isinstance(health, dict) or health.get("service") != "strata":
        raise FitTransportError("polystrata_service_unverified")
    if health.get("loaded") is not True:
        raise FitTransportError("polystrata_model_not_loaded")
    if "model" in health and health.get("model") != served:
        raise FitTransportError("served_model_unverified")
    if (
        "max_context" in health
        and health["max_context"] is not None
        and (type(health["max_context"]) is not int or health["max_context"] != expected_ctx)
    ):
        raise FitTransportError("polystrata_context_mismatch")

    if payload.get("service") != "strata":
        raise FitTransportError("polystrata_service_unverified")
    if "loaded" in payload and payload.get("loaded") is not True:
        raise FitTransportError("polystrata_model_not_loaded")
    if payload.get("model") != served:
        raise FitTransportError("served_model_unverified")
    reported_ctx = _polystrata_context(payload)
    if reported_ctx is None or reported_ctx != expected_ctx:
        raise FitTransportError("polystrata_context_mismatch")
    version = payload.get("engine")
    if not isinstance(version, str) or not re.fullmatch(
        r"[A-Za-z0-9][A-Za-z0-9.+_/\- ]{0,127}", version.strip()
    ):
        raise FitTransportError("engine_version_unverified")
    return {
        "engine": "polystrata",
        "version": version.strip(),
        "served_model": served,
        "max_context": reported_ctx,
        "version_source": VERSION_PATHS["polystrata"],
    }


def sanitize_polystrata_status(raw_body: str) -> dict:
    """Validate and keep only typed queue-admission fields from PolyStrata /status."""
    payload = parse_json_body(raw_body)
    busy = payload.get("busy")
    if type(busy) is not bool or "queued" not in payload:
        raise FitTransportError("polystrata_status_invalid")
    queued = payload["queued"]
    if type(queued) is not int or queued < 0:
        raise FitTransportError("polystrata_status_invalid")
    sanitized: dict[str, bool | int] = {"busy": busy, "queued": queued}
    if "loaded" in payload:
        loaded = payload["loaded"]
        if type(loaded) is not bool:
            raise FitTransportError("polystrata_status_invalid")
        sanitized["loaded"] = loaded
    return sanitized


def service(family, payload, config, *, sources=None, health=None):
    served = config["engine"]["served_model"]
    if family == "polystrata":
        return _polystrata_service(payload, config, health)
    if family == "lmstudio":
        return observer_service(payload, served, FitTransportError)
    if family == "llama-cpp":
        return common_service(family, payload, served, FitTransportError)
    if family == "mlx-lm":
        _ = sources
        if (
            payload.get("local_engine_protocol") != "mlx-lm-formal.v1"
            or payload.get("synthetic") is not False
            or type(payload.get("max_model_len")) is not int
            or payload["max_model_len"] != config["conditions"]["context_size"]
        ):
            raise FitTransportError("mlx_service_protocol_unverified")
    version = payload.get("version")
    if not isinstance(version, str) or not re.fullmatch(r"[0-9][A-Za-z0-9.+_-]{0,127}", version):
        raise FitTransportError("engine_version_unverified")
    return {
        "engine": family,
        "version": version,
        "served_model": served,
        "version_source": VERSION_PATHS[family],
    }


def _polystrata_prometheus_loaded(raw_metrics: str) -> None:
    for line in raw_metrics.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        match = _STRATA_LOADED_METRIC.fullmatch(stripped)
        if match:
            try:
                value = float(match.group(1))
            except ValueError as exc:
                raise FitTransportError("invalid_metrics_value") from exc
            if value != 1.0:
                raise FitTransportError("polystrata_model_not_loaded")


def queue(family, served_model, raw_metrics, observation=None):
    if family == "lmstudio":
        if raw_metrics is not None:
            raise FitTransportError("lmstudio_idle_unverified")
        return observer_idle(observation, served_model, FitTransportError)
    if family == "polystrata":
        if raw_metrics is not None:
            if not isinstance(raw_metrics, str):
                raise FitTransportError("missing_idle_metrics")
            _polystrata_prometheus_loaded(raw_metrics)
            return _idle_metrics("llama-cpp", raw_metrics)
        if not isinstance(observation, dict) or type(observation.get("busy")) is not bool:
            raise FitTransportError("polystrata_status_invalid")
        if "loaded" in observation and observation["loaded"] is not True:
            raise FitTransportError("polystrata_model_not_loaded")
        if "queued" not in observation:
            raise FitTransportError("polystrata_status_invalid")
        queued = observation["queued"]
        if type(queued) is not int or queued < 0:
            raise FitTransportError("polystrata_status_invalid")
        busy = observation["busy"]
        return {
            "idle": (not busy) and (queued == 0),
            "busy": busy,
            "queued": queued,
        }
    if not isinstance(raw_metrics, str):
        raise FitTransportError("missing_idle_metrics")
    return _idle_metrics(family, raw_metrics)
