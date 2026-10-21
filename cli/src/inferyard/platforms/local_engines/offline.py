"""Validate sealed local identities without reopening paths or observing services."""

import hashlib
import os
from pathlib import PurePosixPath

from inferyard.contracts.engine_profiles import DIRECTORY_FAMILIES
from inferyard.contracts.local_environment import validate_installation_environment
from inferyard.contracts.schemas_common import HASH, LABEL, NAT, array, obj
from inferyard.contracts.validation import ContractError, _validate
from inferyard.evidence.storage import json_bytes
from inferyard.platforms.identity import PreflightError
from inferyard.platforms.local_engines.binding_schema import validate as validate_binding
from inferyard.platforms.local_engines.runtime_schema import RUNTIME
from inferyard.platforms.local_engines.startup import validate_arguments

_ENTRY = obj({"path": LABEL, "sha256": HASH, "size": NAT})


def _require(value):
    if not value:
        raise ContractError("identity.local_engine", "frozen local identity mismatch")


def validate_frozen_identity(config, local_identity, *, files):
    """Check canonical document hashes, closed fields and model/file correspondence."""
    _require(
        isinstance(local_identity, dict)
        and set(local_identity) == {"runtime", "binding", "model_assets"}
    )
    runtime, binding, manifest = (local_identity[k] for k in ("runtime", "binding", "model_assets"))
    _validate(runtime, RUNTIME, "identity.local_engine.runtime")
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
        bootstrap_path = None
        if engine["family"] in DIRECTORY_FAMILIES:
            arguments = engine["startup_args"]
            _require(len(arguments) > 4)
            bootstrap_path = arguments[3]
            path = PurePosixPath(bootstrap_path)
            _require(
                path.is_absolute()
                and str(path) == bootstrap_path
                and path.parts[-3:] == ("data", "local_engine", "bootstrap.py")
                and ".." not in path.parts
            )
            matches = []
            for root in runtime["roots"]:
                root_path = PurePosixPath(root["path"])
                for row in root["entries"]:
                    if (
                        root_path / row["path"] == path
                        or root_path == path
                        and row["path"] == path.name
                    ):
                        matches.append(row)
            _require(len(matches) == 1)
        try:
            validate_arguments(config, bootstrap_path=bootstrap_path)
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
    runtime_digests = set()
    root_names = set()
    for root in runtime["roots"]:
        _require(PurePosixPath(root["path"]).is_absolute() and root["path"] not in root_names)
        root_names.add(root["path"])
        entry_names = [row["path"] for row in root["entries"]]
        _require(entry_names == sorted(set(entry_names)))
        for row in root["entries"]:
            _require(
                not PurePosixPath(row["path"]).is_absolute()
                and all(part not in ("", ".", "..") for part in row["path"].split("/"))
            )
            runtime_digests.add(row["sha256"])
    _require(service["executable_sha256"] in runtime_digests)
    _require(all(row["executable_sha256"] in runtime_digests for row in service["workers"]))
    properties = {"kind": LABEL, "path": LABEL, "sha256": HASH, "files": array(_ENTRY, 1)}
    if model["kind"] == "directory":
        properties["definition"] = {"type": "string", "const": "model-assets.v2"}
    _validate(manifest, obj(properties), "identity.local_engine.model_assets")
    _require(manifest["kind"] == model["kind"] and manifest["path"] == model["local_path"])
    _require(
        manifest["sha256"]
        == model["content_sha256"]
        == hashlib.sha256(json_bytes(manifest["files"])).hexdigest()
    )
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
        (manifest, model["assets_path"], model["assets_sha256"]),
    )
    for document, path, expected in documents:
        content = json_bytes(document)
        _require(hashlib.sha256(content).hexdigest() == expected)
        _require(inventory.get(path) == (expected, len(content)))
    _require(inventory.get(model["template_path"], (None,))[0] == model["template_sha256"])
    names = [entry["path"] for entry in manifest["files"]]
    _require(names == sorted(set(names)))
    base = PurePosixPath(model["local_path"])
    for entry in manifest["files"]:
        relative = PurePosixPath(entry["path"])
        _require(
            not relative.is_absolute()
            and all(p not in ("", ".", "..") for p in entry["path"].split("/"))
            and "\\" not in entry["path"]
        )
        path = str(base / relative) if model["kind"] == "directory" else str(base)
        _require(inventory.get(path) == (entry["sha256"], entry["size"]))
    if model["kind"] == "gguf":
        _require(names == [base.name])
    return service
