"""Pure validation for formal local service observations and queue samples."""

import hashlib
import re
from importlib.resources import files

from inferyard.adapters.engine_fit import FitTransportError, _idle_metrics, _json
from inferyard.adapters.engine_fit_common import common_service, observer_idle, observer_service

FAMILIES = ("llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio")
VERSION_PATHS = {
    "llama-cpp": "/props",
    "mlx-lm": "/version",
    "vllm": "/version",
    "sglang": "/get_server_info",
}


def mlx_sources():
    return {
        key: hashlib.sha256(files("inferyard").joinpath(path).read_bytes()).hexdigest()
        for key, path in (
            ("local_engine_source_sha256", "data/local_engine/mlx_server.py"),
            ("generation_queue_source_sha256", "data/engine_fit/mlx_server.py"),
        )
    }


def models(raw, served_model):
    listing = _json(raw).get("data")
    if (
        not isinstance(listing, list)
        or len(listing) != 1
        or not isinstance(listing[0], dict)
        or listing[0].get("id") != served_model
    ):
        raise FitTransportError("served_model_unverified")


def service(family, payload, config, *, sources=None):
    served = config["engine"]["served_model"]
    if family == "lmstudio":
        return observer_service(payload, served, FitTransportError)
    if family == "llama-cpp":
        return common_service(family, payload, served, FitTransportError)
    if family == "mlx-lm":
        if (
            payload.get("local_engine_protocol") != "mlx-lm-formal.v1"
            or payload.get("synthetic") is not False
            or type(payload.get("max_model_len")) is not int
            or payload["max_model_len"] != config["conditions"]["context_size"]
            or any(payload.get(key) != value for key, value in (sources or mlx_sources()).items())
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


def queue(family, served_model, raw_metrics, observation=None):
    if family == "lmstudio":
        if raw_metrics is not None:
            raise FitTransportError("lmstudio_idle_unverified")
        return observer_idle(observation, served_model, FitTransportError)
    if not isinstance(raw_metrics, str):
        raise FitTransportError("missing_idle_metrics")
    return _idle_metrics(family, raw_metrics)
