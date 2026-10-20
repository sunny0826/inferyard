"""Hash-bound manifests retain the existing model-assets.v2 byte algorithm."""

from pathlib import Path

from inferyard.evidence.storage import json_bytes
from inferyard.platforms.identity import PreflightError, hash_file
from inferyard.platforms.local_engines.installation import read_json, verify_installation
from inferyard.platforms.local_engines.model_snapshot import snapshot as model_snapshot


def bound_json(path, digest):
    identity = hash_file(Path(path), digest)
    value = read_json(path)
    try:
        if Path(path).read_bytes() != json_bytes(value):
            raise PreflightError("local_engine_frozen_json_not_canonical")
    except OSError as exc:
        raise PreflightError("local_engine_json_unreadable") from exc
    hash_file(Path(path), digest)
    return value, identity


def assets(config):
    engine, model = config["engine"], config["model"]
    runtime, runtime_file = bound_json(engine["installation_path"], engine["installation_sha256"])
    runtime_files = verify_installation(runtime)
    declaration = runtime["installation"]
    if declaration["family"] != engine["family"]:
        raise PreflightError("local_engine_family_mismatch")
    if engine.get("environment", declaration["environment"]) != declaration["environment"]:
        raise PreflightError("local_engine_environment_mismatch")
    frozen, manifest_file = bound_json(model["assets_path"], model["assets_sha256"])
    current, model_files = model_snapshot(Path(model["local_path"]))
    if (
        frozen != current
        or current["sha256"] != model["content_sha256"]
        or current["kind"] != model["kind"]
    ):
        raise PreflightError("local_engine_model_assets_changed")
    template_file = hash_file(Path(model["template_path"]), model["template_sha256"])
    return runtime, runtime_files, [runtime_file, manifest_file, template_file, *model_files]


def preflight_assets(config):
    from inferyard.platforms.local_engines.service import validate_binding

    runtime, _, files = assets(config)
    endpoint = config["endpoint"]
    binding, binding_file = bound_json(endpoint["binding_path"], endpoint["binding_sha256"])
    validate_binding(binding, config)
    model = config["model"]
    model_assets, _ = bound_json(model["assets_path"], model["assets_sha256"])
    return {
        "local_engine": {"runtime": runtime, "binding": binding, "model_assets": model_assets}
    }, [*files, binding_file]
