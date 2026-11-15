"""Frozen profile projection; never inspects current hardware or weight files."""

from pathlib import PureWindowsPath

GENERIC_TITLES = {"hf", "unknown", "local", "模型"}


def recorded_integer(value):
    return value if type(value) is int and value >= 0 else None


def _text(value):
    return value if type(value) is str and value else None


def _title(model):
    display = model["display_name"]
    if display.lower() not in GENERIC_TITLES:
        return display
    stem = PureWindowsPath(model["local_path"]).stem
    packing = model["packing"]
    if stem.endswith("-" + packing):
        stem = stem[: -len(packing) - 1]
    return stem.replace("-", " ") or None


def profile_view(data):
    config, engine = data["config"], data["config"]["engine"]
    environment = data.get("environment_start") or {}
    return {
        "title": _title(config["model"]),
        "engine": {
            "binary_path": _text(engine.get("binary_path")),
            "adapter": engine["adapter"],
            "backend": engine["backend"],
        },
        "machine": {
            "cpu": _text(environment.get("cpu_model")),
            "logical_cpus": recorded_integer(environment.get("logical_cpus")),
            "memory_total_bytes": recorded_integer(environment.get("memory_total_bytes")),
        },
    }
