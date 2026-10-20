"""Load and normalize local configuration, without networking or probing a service."""

from __future__ import annotations

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


def path_fields(config):
    from inferyard.contracts.engine_profiles import is_local

    if not is_local(config):
        return PATH_FIELDS
    return tuple(
        (section, key)
        for section, key in (
            ("model", "local_path"),
            ("model", "template_path"),
            ("model", "assets_path"),
            ("engine", "installation_path"),
            ("engine", "working_directory"),
            ("endpoint", "binding_path"),
            ("bundle", "path"),
            ("output", "root"),
        )
        if key in config[section]
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
    source: Path | None
    config: Document
    bundle: Document
    source_data: bytes | None = None
    source_media_type: str | None = None
    source_missing_reason: str | None = None

    def configuration_source(self) -> dict:
        if self.source_data is not None:
            return {
                "media_type": self.source_media_type,
                "data": self.source_data,
                "missing_reason": None,
            }
        if self.source_missing_reason is not None:
            return {"media_type": None, "data": None, "missing_reason": self.source_missing_reason}
        if self.source is not None:
            return {
                "media_type": "application/toml"
                if self.source.suffix == ".toml"
                else "application/json",
                "data": read_document(self.source),
                "missing_reason": None,
            }
        return {
            "media_type": None,
            "data": None,
            "missing_reason": self.source_missing_reason or "configuration_source_unavailable",
        }


def validate_runtime_config(config: dict) -> None:
    """Apply credential and capacity rules to TOML and frozen JSON alike."""
    from inferyard.evidence.formats import require_input

    require_input(config, "config")
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
    from inferyard.evidence.formats import require_input

    require_input(incoming, "config")
    validate_document("config_input", incoming)
    normalized = deepcopy(incoming)
    for section, defaults in CONFIG_DEFAULTS.items():
        target = normalized.setdefault(section, {})
        for key, value in defaults.items():
            if key not in target:
                target[key] = deepcopy(value)
    validate_runtime_config(normalized)
    if normalized["engine"]["adapter"] in ("kvmem", "ninfer"):
        import os
        from pathlib import PureWindowsPath

        if os.name != "nt":
            # Offline plans may freeze Windows config on another platform.
            paths = [
                (section, key)
                for section, key in path_fields(normalized)
                if not PureWindowsPath(normalized[section][key]).is_absolute()
            ]
        else:
            paths = path_fields(normalized)
    else:
        paths = path_fields(normalized)
    for section, key in paths:
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
    require_input(bundle_data, "bundle")
    bundle = Document.parse("bundle", bundle_data)
    if bundle_data["version"] != normalized["bundle"]["version"]:
        raise ContractError("config.bundle.version", "does not match corpus version")
    return LoadedConfig(
        source, Document.parse("config", normalized), bundle, raw, "application/toml"
    )
