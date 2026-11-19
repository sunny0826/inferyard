"""Hash-bound manifests retain the existing model-assets.v2 byte algorithm."""

import hashlib
from pathlib import Path

from inferyard.errors import PreflightError
from inferyard.contracts.validation import json_bytes, strict_json_loads
from inferyard.platforms.identity import hash_file
from inferyard.platforms.local_engines.installation import verify_installation
from inferyard.platforms.local_engines.model_snapshot import snapshot as model_snapshot


def _parse(raw):
    try:
        return strict_json_loads(raw.decode("utf-8"))
    except (UnicodeError, ValueError) as exc:
        raise PreflightError("local_engine_json_unreadable") from exc


def bound_json(path, digest):
    identity = hash_file(Path(path), digest)
    try:
        raw = Path(path).read_bytes()
    except OSError as exc:
        raise PreflightError("local_engine_json_unreadable") from exc
    if hashlib.sha256(raw).hexdigest() != identity.sha256:
        raise PreflightError("identity_file_changed_during_hash")
    value = _parse(raw)
    if raw != json_bytes(value):
        raise PreflightError("local_engine_frozen_json_not_canonical")
    return value, identity


def _inspect(config):
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
    files = [runtime_file, manifest_file, template_file, *model_files]
    return runtime, runtime_files, files, frozen


def assets(config):
    runtime, runtime_files, files, _ = _inspect(config)
    return runtime, runtime_files, files


def preflight_assets(config):
    from inferyard.platforms.local_engines.service import validate_binding

    runtime, _, files, model_assets = _inspect(config)
    endpoint = config["endpoint"]
    binding, binding_file = bound_json(endpoint["binding_path"], endpoint["binding_sha256"])
    validate_binding(binding, config)
    return {
        "local_engine": {"runtime": runtime, "binding": binding, "model_assets": model_assets}
    }, [*files, binding_file]
