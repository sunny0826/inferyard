"""Validate sealed local identities without reopening paths or observing services."""

import hashlib
import os
from pathlib import PurePosixPath

from inferyard.contracts.local_environment import (
    is_absolute_path,
    parse_absolute_path,
    validate_installation_environment,
)
from inferyard.contracts.validation import validate_value, json_bytes
from inferyard.errors import ContractError, PreflightError
from inferyard.platforms.local_engines.binding_schema import validate as validate_binding
from inferyard.platforms.local_engines.runtime_schema import RUNTIME
from inferyard.platforms.local_engines.startup import validate_arguments


def _require(value):
    if not value:
        raise ContractError("identity.local_engine", "frozen local identity mismatch")


def validate_frozen_identity(config, local_identity, *, files):
    """Check canonical document hashes, closed fields and binding correspondence."""
    _require(
        isinstance(local_identity, dict)
        and {"runtime", "binding"} <= set(local_identity) <= {"runtime", "binding", "model_assets"}
    )
    runtime, binding = (local_identity[k] for k in ("runtime", "binding"))
    validate_value(runtime, RUNTIME, "identity.local_engine.runtime")
    try:
        service = validate_binding(binding, config)
    except PreflightError as exc:
        raise ContractError("identity.local_engine.binding", "frozen binding mismatch") from exc
    engine, model = config["engine"], config["model"]
    declaration = runtime["installation"]
    validate_installation_environment(declaration)
    _require(declaration["family"] == engine["family"])
    _require(declaration["environment"] == engine["environment"])
    if engine["family"] != "lmstudio":
        _require(declaration["working_directory"] == engine["working_directory"])
        try:
            validate_arguments(config)
        except PreflightError as exc:
            raise ContractError("identity.local_engine", "invalid frozen startup") from exc
    arguments = engine["startup_args"]
    if engine["family"] != "lmstudio":
        arguments = [declaration["executable"], *arguments]
    raw = b"\0".join(os.fsencode(arg) for arg in arguments) + b"\0"
    _require(service["argv_sha256"] == hashlib.sha256(raw).hexdigest())
    _require(
        service["cwd_sha256"]
        == hashlib.sha256(os.fsencode(engine["working_directory"])).hexdigest()
    )
    root_names = set()
    for root in runtime["roots"]:
        _require(is_absolute_path(root["path"]) and root["path"] not in root_names)
        root_names.add(root["path"])
        entry_names = [row["path"] for row in root["entries"]]
        _require(entry_names == sorted(set(entry_names)))
        for row in root["entries"]:
            _require(
                not PurePosixPath(row["path"]).is_absolute()
                and all(part not in ("", ".", "..") for part in row["path"].split("/"))
            )
    _require(model["kind"] in ("gguf", "directory"))
    _require(parse_absolute_path(model["local_path"]) is not None)
    inventory = {}
    for item in files:
        path = item["path"] if isinstance(item, dict) else item.path
        sha = item["sha256"] if isinstance(item, dict) else item.sha256
        size = item["size"] if isinstance(item, dict) else item.size
        _require(path not in inventory or inventory[path] == (sha, size))
        inventory[path] = (sha, size)
    documents = (
        (runtime, engine["installation_path"], engine["installation_sha256"]),
        (binding, config["endpoint"]["binding_path"], config["endpoint"]["binding_sha256"]),
    )
    for document, path, expected in documents:
        content = json_bytes(document)
        _require(hashlib.sha256(content).hexdigest() == expected)
        _require(inventory.get(path) == (expected, len(content)))
    _require(inventory.get(model["template_path"], (None,))[0] == model["template_sha256"])
    if engine["family"] == "polystrata":
        _require(
            inventory.get(engine["polystrata_config_path"], (None,))[0]
            == engine["polystrata_config_sha256"]
        )
    return service
