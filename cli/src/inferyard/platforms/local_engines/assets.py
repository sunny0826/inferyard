"""Lightweight local engine runtime, template, and binding verification without weight hashing."""

import hashlib
from pathlib import Path

from inferyard.errors import PreflightError
from inferyard.contracts.validation import json_bytes, strict_json_loads
from inferyard.platforms.identity import FileIdentity, hash_file
from inferyard.platforms.local_engines.installation import verify_installation

type JsonScalar = str | int | float | bool | None
type JsonTree = JsonScalar | list[JsonTree] | dict[str, JsonTree]
type JsonObject = dict[str, JsonTree]


def _parse(raw: bytes) -> JsonObject:
    try:
        return strict_json_loads(raw.decode("utf-8"))
    except (UnicodeError, ValueError) as exc:
        raise PreflightError("local_engine_json_unreadable") from exc


def bound_json(path: str | Path, digest: str | None = None) -> tuple[JsonObject, FileIdentity]:
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


def _verify_polystrata_config(config: JsonObject, declaration: JsonObject) -> FileIdentity:
    from inferyard.config.local_engines.polystrata import (
        UPSTREAM_DEFAULT_CONTEXT,
        inspect_polystrata_payload,
    )
    from inferyard.provisioning.preparation_io import PreparationError

    engine = config["engine"]
    ps_path = Path(engine["polystrata_config_path"])
    ps_file = hash_file(ps_path, engine["polystrata_config_sha256"])
    try:
        raw = ps_path.read_bytes()
    except OSError as exc:
        raise PreflightError("local_engine_json_unreadable") from exc
    if hashlib.sha256(raw).hexdigest() != ps_file.sha256:
        raise PreflightError("identity_file_changed_during_hash")
    payload = _parse(raw)
    if not isinstance(payload, dict):
        raise PreflightError("local_engine_json_unreadable")
    bases: list[Path] = [Path(declaration["working_directory"]), ps_path.parent]
    if declaration.get("data_dir"):
        bases.append(Path(declaration["data_dir"]))
    try:
        asset_path, ctx, _, _ = inspect_polystrata_payload(payload, ps_path, tuple(bases))
    except PreparationError as exc:
        raise PreflightError(exc.reason) from exc
    expected_model = Path(config["model"]["local_path"]).resolve(strict=True)
    if asset_path is None:
        if (ps_path.parent / "tokenizer_config.json").is_file() or (
            ps_path.parent / "chat_template.jinja"
        ).is_file():
            asset_path = ps_path.parent.resolve(strict=True)
    if asset_path is None or asset_path != expected_model:
        raise PreflightError("local_engine_model_argument_mismatch")
    conditions = config.get("conditions")
    effective_ctx = ctx if ctx is not None else UPSTREAM_DEFAULT_CONTEXT
    if (
        isinstance(conditions, dict)
        and "context_size" in conditions
        and effective_ctx != conditions["context_size"]
    ):
        raise PreflightError("local_engine_context_argument_mismatch")
    return ps_file


def _verify_model_path(model: JsonObject) -> None:
    try:
        model_path = Path(model["local_path"])
        is_dir = model_path.is_dir()
        is_file = model_path.is_file()
    except OSError as exc:
        raise PreflightError("local_engine_model_unreadable") from exc
    expected_kind = model["kind"]
    if (expected_kind == "directory" and not is_dir) or (expected_kind == "gguf" and not is_file):
        raise PreflightError("local_engine_model_unreadable")


def _inspect(
    config: JsonObject,
) -> tuple[JsonObject, list[FileIdentity], list[FileIdentity]]:
    engine, model = config["engine"], config["model"]
    runtime, runtime_file = bound_json(engine["installation_path"], engine["installation_sha256"])
    runtime_files = verify_installation(runtime)
    declaration = runtime["installation"]
    if declaration["family"] != engine["family"]:
        raise PreflightError("local_engine_family_mismatch")
    if engine.get("environment", declaration["environment"]) != declaration["environment"]:
        raise PreflightError("local_engine_environment_mismatch")
    extra_files: list[FileIdentity] = []
    if engine["family"] == "polystrata":
        extra_files.append(_verify_polystrata_config(config, declaration))
    _verify_model_path(model)
    template_file = hash_file(Path(model["template_path"]), model["template_sha256"])
    files = [runtime_file, template_file, *extra_files]
    return runtime, runtime_files, files


def assets(config: JsonObject) -> tuple[JsonObject, list[FileIdentity], list[FileIdentity]]:
    return _inspect(config)


def preflight_assets(config: JsonObject) -> tuple[JsonObject, list[FileIdentity]]:
    from inferyard.platforms.local_engines.service import validate_binding

    runtime, _, files = _inspect(config)
    endpoint = config["endpoint"]
    binding, binding_file = bound_json(endpoint["binding_path"], endpoint["binding_sha256"])
    validate_binding(binding, config)
    return {"local_engine": {"runtime": runtime, "binding": binding}}, [*files, binding_file]
