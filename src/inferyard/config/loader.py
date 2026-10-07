"""Load and normalize local configuration, without networking or probing a service."""

from __future__ import annotations

import hashlib
import ipaddress
import tomllib
from copy import deepcopy
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlsplit

from inferyard.contracts.schemas import CONFIG_DEFAULTS
from inferyard.contracts.validation import (
    ContractError,
    Document,
    strict_json_loads,
    validate_document,
)

MAX_DOCUMENT_BYTES = 16 * 1024 * 1024
PATH_FIELDS = (
    ("model", "local_path"),
    ("model", "template_path"),
    ("engine", "binary_path"),
    ("engine", "runtime_library_manifest"),
    ("bundle", "path"),
    ("output", "root"),
)


def read_document(path: Path) -> bytes:
    # Bound config/corpus reads; model files are not read in this module.
    with path.open("rb") as stream:
        raw = stream.read(MAX_DOCUMENT_BYTES + 1)
    if len(raw) > MAX_DOCUMENT_BYTES:
        raise ContractError("document", "exceeds size limit")
    return raw


def validate_endpoint(url: str) -> None:
    try:
        parts = urlsplit(url)
        host = parts.hostname
        port = parts.port
        if (
            parts.scheme not in ("http", "https")
            or host is None
            or parts.username is not None
            or parts.password is not None
            or parts.query
            or parts.fragment
            or "?" in url
            or "#" in url
            or any(character.isspace() for character in url)
            or parts.path not in ("", "/")
            or port == 0
        ):
            raise ValueError
        # A future transport must pin localhost resolution to loopback too.
        if host != "localhost" and not ipaddress.ip_address(host).is_loopback:
            raise ValueError
    except ValueError as exc:
        raise ContractError(
            "config.endpoint.url", "requires a loopback origin without credentials"
        ) from exc


@dataclass(frozen=True, slots=True)
class LoadedConfig:
    source: Path
    config: Document
    bundle: Document
    input_sha256: str
    bundle_sha256: str
    defaulted_fields: tuple[str, ...]


def validate_runtime_config(config: dict) -> None:
    """Apply credential and capacity rules to TOML and frozen JSON alike."""
    validate_document("config", config)
    validate_endpoint(config["endpoint"]["url"])
    for index, argument in enumerate(config["engine"]["startup_args"]):
        option = argument.split("=", 1)[0].lower()
        if option in ("--api-key", "--api-key-file", "--password", "--token", "-hft", "--hf-token"):
            raise ContractError(
                f"config.engine.startup_args[{index}]",
                "credential arguments are not allowed; use a reference",
            )
    if config["generation"]["max_tokens"] > config["conditions"]["context_size"]:
        raise ContractError("config.generation.max_tokens", "exceeds total context size")


def load_config(path: str | Path) -> LoadedConfig:
    source = Path(path).resolve()
    raw = read_document(source)
    try:
        incoming = tomllib.loads(raw.decode("utf-8"))
    except (tomllib.TOMLDecodeError, UnicodeError) as exc:
        # TOMLDecodeError messages may contain rejected values (including secrets).
        raise ContractError("config", "invalid UTF-8 TOML syntax") from exc
    validate_document("config_input", incoming)
    normalized = deepcopy(incoming)
    defaulted = []
    for section, defaults in CONFIG_DEFAULTS.items():
        target = normalized.setdefault(section, {})
        for key, value in defaults.items():
            if key not in target:
                target[key] = deepcopy(value)
                defaulted.append(f"{section}.{key}")
    validate_runtime_config(normalized)
    if normalized["engine"]["adapter"] in ("kvmem", "ninfer"):
        import os
        from pathlib import PureWindowsPath

        if os.name != "nt":
            # Offline plans may freeze Windows config on another platform.
            path_fields = [
                (section, key)
                for section, key in PATH_FIELDS
                if not PureWindowsPath(normalized[section][key]).is_absolute()
            ]
        else:
            path_fields = PATH_FIELDS
    else:
        path_fields = PATH_FIELDS
    for section, key in path_fields:
        value = Path(normalized[section][key])
        normalized[section][key] = str((source.parent / value).resolve())
    if "component_ledger_path" in normalized["model"] and normalized["engine"]["adapter"] not in (
        "kvmem",
        "ninfer",
    ):
        normalized["model"]["component_ledger_path"] = str(
            (source.parent / normalized["model"]["component_ledger_path"]).resolve()
        )
    for key in ("parameter_evidence", "service_evidence"):
        normalized["evidence"][key] = [
            str((source.parent / value).resolve()) for value in normalized["evidence"][key]
        ]
    bundle_raw = read_document(Path(normalized["bundle"]["path"]))
    try:
        bundle_data = strict_json_loads(bundle_raw.decode("utf-8"))
    except UnicodeError as exc:
        raise ContractError("bundle", "invalid UTF-8") from exc
    bundle = Document.parse("bundle", bundle_data)
    if bundle_data["version"] != normalized["bundle"]["version"]:
        raise ContractError("config.bundle.version", "does not match corpus version")
    return LoadedConfig(
        source,
        Document.parse("config", normalized),
        bundle,
        hashlib.sha256(raw).hexdigest(),
        hashlib.sha256(bundle_raw).hexdigest(),
        tuple(defaulted),
    )
