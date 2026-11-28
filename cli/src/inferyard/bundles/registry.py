"""Discover, load, and verify benchmark bundles from packaged or repo benchmarks/."""

from __future__ import annotations

import hashlib
import re
from importlib.resources import files
from pathlib import Path, PurePosixPath

from inferyard.contracts.bundle import content_hash, validate_bundle
from inferyard.contracts.context_speed_profile import (
    BUNDLE_CONTENT_SHA256 as CONTEXT_SPEED_CONTENT_SHA256,
    BUNDLE_ID as CONTEXT_SPEED_BUNDLE_ID,
    BUNDLE_VERSION as CONTEXT_SPEED_BUNDLE_VERSION,
    CASES as CONTEXT_SPEED_CASES,
    PROFILE as CONTEXT_SPEED_PROFILE,
)
from inferyard.contracts.schemas_tasks import EXECUTION_HINTS
from inferyard.contracts.validation import strict_json_loads, validate_value
from inferyard.errors import ContractError
from inferyard.provisioning.preparation_io import PreparationError

_VALID_NAME = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$")
_CONTEXT_SPEED_CASE_IDS = frozenset(CONTEXT_SPEED_CASES.values())


def _safe_rel_path(name: str) -> PurePosixPath:
    if not isinstance(name, str) or not name or "\\" in name or "\x00" in name:
        raise PreparationError("package_resource_invalid", 4)
    rel = PurePosixPath(name)
    if (
        rel.is_absolute()
        or not rel.parts
        or any(part in ("", ".", "..") or ":" in part for part in rel.parts)
        or rel.as_posix() != name
    ):
        raise PreparationError("package_resource_invalid", 4)
    return rel


def _benchmarks_root():
    packaged = files("inferyard").joinpath("data/benchmarks")
    try:
        if packaged.joinpath("catalog.json").is_file():
            return packaged
    except (OSError, ValueError):
        pass
    # Editable checkout fallback: <repo>/cli/src/inferyard/bundles/registry.py -> <repo>/benchmarks
    for parent in Path(__file__).resolve().parents:
        candidate = parent / "benchmarks"
        if (candidate / "catalog.json").is_file():
            return candidate
    raise PreparationError("package_resource_invalid", 4)


def _read_resource_bytes(rel_path: str) -> bytes:
    rel = _safe_rel_path(rel_path)
    root = _benchmarks_root()
    try:
        return root.joinpath(*rel.parts).read_bytes()
    except (OSError, ValueError):
        raise PreparationError("package_resource_invalid", 4) from None


def catalog() -> dict:
    raw = _read_resource_bytes("catalog.json")
    try:
        doc = strict_json_loads(raw.decode("utf-8"))
    except (UnicodeError, ContractError) as exc:
        raise PreparationError("package_resource_invalid", 4) from exc
    if type(doc) is not dict or doc.get("schema_version") != 1:
        raise PreparationError("package_resource_invalid", 4)
    entries = doc.get("bundles")
    if type(entries) is not list or not entries:
        raise PreparationError("package_resource_invalid", 4)
    seen = set()
    for item in entries:
        if type(item) is not dict:
            raise PreparationError("package_resource_invalid", 4)
        name = item.get("name")
        rel = item.get("file")
        if (
            not isinstance(name, str)
            or not _VALID_NAME.fullmatch(name)
            or name in seen
            or not isinstance(rel, str)
        ):
            raise PreparationError("package_resource_invalid", 4)
        rel_path = _safe_rel_path(rel)
        if (
            len(rel_path.parts) != 2
            or rel_path.parts[0] != "bundles"
            or not rel_path.parts[1].endswith(".json")
        ):
            raise PreparationError("package_resource_invalid", 4)
        if "execution_hints" in item:
            try:
                validate_value(item["execution_hints"], EXECUTION_HINTS, "catalog.execution_hints")
            except ContractError as exc:
                raise PreparationError("package_resource_invalid", 4) from exc
        seen.add(name)
    return doc


def catalog_entries() -> tuple[dict, ...]:
    return tuple(catalog()["bundles"])


def bundle_names() -> tuple[str, ...]:
    return tuple(entry["name"] for entry in catalog_entries())


def default_bundles() -> tuple[str, ...]:
    defaults = tuple(entry["name"] for entry in catalog_entries() if entry.get("default") is True)
    return defaults or bundle_names()[:1]


def catalog_entry(name_or_id: str) -> dict | None:
    for entry in catalog_entries():
        if entry["name"] == name_or_id:
            return entry
    # Fallback match by alias / bundle_id inside registered bundles.
    for entry in catalog_entries():
        try:
            doc = strict_json_loads(_read_resource_bytes(entry["file"]).decode("utf-8"))
        except (PreparationError, UnicodeError, ContractError):
            continue
        if type(doc) is dict and doc.get("bundle_id") == name_or_id:
            return entry
    return None


def bundle_bytes(name: str) -> bytes:
    if not isinstance(name, str) or not _VALID_NAME.fullmatch(name):
        raise PreparationError("package_resource_invalid", 4)
    for entry in catalog_entries():
        if entry["name"] == name:
            return _read_resource_bytes(entry["file"])
    raise PreparationError("package_resource_invalid", 4)


def _derive_local_name(path: Path, bundle: dict) -> str:
    stem = path.stem
    if _VALID_NAME.fullmatch(stem):
        return stem
    bundle_id = bundle.get("bundle_id")
    if isinstance(bundle_id, str) and _VALID_NAME.fullmatch(bundle_id):
        return bundle_id
    raise PreparationError("invalid_input")


def resolve_bundle_spec(spec: str) -> tuple[str, bytes, dict]:
    """Resolve either a registered benchmark name or a local bundle JSON path."""
    if not isinstance(spec, str) or not spec.strip():
        raise PreparationError("invalid_input")
    if spec in bundle_names():
        raw = bundle_bytes(spec)
        try:
            doc = strict_json_loads(raw.decode("utf-8"))
            validate_bundle(doc)
        except (UnicodeError, ContractError, ValueError) as exc:
            raise PreparationError("package_resource_invalid", 4) from exc
        return spec, raw, doc

    path = Path(spec).expanduser()
    try:
        resolved = path.resolve(strict=True)
    except OSError:
        raise PreparationError("invalid_input") from None
    if not resolved.is_file():
        raise PreparationError("invalid_input")
    try:
        raw = resolved.read_bytes()
        doc = strict_json_loads(raw.decode("utf-8"))
        validate_bundle(doc)
    except (OSError, UnicodeError, ContractError, ValueError) as exc:
        raise PreparationError("invalid_input") from exc
    return _derive_local_name(resolved, doc), raw, doc


def effective_execution_hints(bundle: dict, name: str | None = None) -> dict:
    hints: dict = {}
    lookup = name or (bundle.get("bundle_id") if isinstance(bundle, dict) else None)
    if isinstance(lookup, str):
        entry = catalog_entry(lookup)
        if entry and isinstance(entry.get("execution_hints"), dict):
            hints.update(entry["execution_hints"])
    inline = bundle.get("execution_hints") if isinstance(bundle, dict) else None
    if isinstance(inline, dict):
        hints.update(inline)
    return hints


def effective_metadata(bundle: dict, name: str | None = None) -> dict:
    meta: dict = {}
    lookup = name or (bundle.get("bundle_id") if isinstance(bundle, dict) else None)
    if isinstance(lookup, str):
        entry = catalog_entry(lookup)
        if entry:
            for key in ("display_name", "description", "tags"):
                if key in entry:
                    meta[key] = entry[key]
    inline = bundle.get("metadata") if isinstance(bundle, dict) else None
    if isinstance(inline, dict):
        meta.update(inline)
    return meta


def is_context_speed_bundle(bundle: dict, name: str | None = None) -> bool:
    if effective_execution_hints(bundle, name).get("profile") == CONTEXT_SPEED_PROFILE:
        return True
    if not isinstance(bundle, dict):
        return False
    cases = bundle.get("cases")
    if isinstance(cases, list) and any(
        isinstance(case, dict) and case.get("case_id") in _CONTEXT_SPEED_CASE_IDS
        for case in cases
    ):
        return True
    return False


def _verify_one(name: str, raw: bytes, source: str, entry: dict | None = None) -> dict:
    try:
        doc = strict_json_loads(raw.decode("utf-8"))
    except (UnicodeError, ContractError) as exc:
        raise ContractError(f"bundle.{name}", f"invalid JSON: {exc}") from exc
    validate_bundle(doc)
    cases = doc["cases"]
    categories = sorted({case["category"] for case in cases})
    meta = effective_metadata(doc, name)
    hints = effective_execution_hints(doc, name)
    digest = content_hash(doc)
    if hints.get("profile") == CONTEXT_SPEED_PROFILE:
        if (
            doc["task_protocol"] != "performance"
            or doc["bundle_id"] != CONTEXT_SPEED_BUNDLE_ID
            or doc["version"] != CONTEXT_SPEED_BUNDLE_VERSION
            or digest != CONTEXT_SPEED_CONTENT_SHA256
        ):
            raise ContractError(
                f"bundle.{name}.execution_hints.profile",
                "context-speed.v1 profile requires the frozen zh-context-speed bundle identity",
            )
    return {
        "name": name,
        "bundle_id": doc["bundle_id"],
        "version": doc["version"],
        "language": doc["language"],
        "task_protocol": doc["task_protocol"],
        "case_count": len(cases),
        "categories": categories,
        "default": bool(entry.get("default")) if entry else False,
        "display_name": meta.get("display_name"),
        "description": meta.get("description"),
        "tags": meta.get("tags", []),
        "execution_hints": hints,
        "content_sha256": digest,
        "file_sha256": hashlib.sha256(raw).hexdigest(),
        "source": source,
    }


def list_bundles() -> dict:
    items = []
    for entry in catalog_entries():
        raw = _read_resource_bytes(entry["file"])
        items.append(_verify_one(entry["name"], raw, entry["file"], entry))
    return {
        "schema_version": 1,
        "default_bundles": list(default_bundles()),
        "items": items,
    }


def verify_bundles(target: Path | None = None) -> dict:
    if target is not None:
        resolved = target.expanduser().resolve(strict=True)
        if resolved.is_file():
            raw = resolved.read_bytes()
            doc = strict_json_loads(raw.decode("utf-8"))
            validate_bundle(doc)
            name = _derive_local_name(resolved, doc)
            return {"verified": True, "items": [_verify_one(name, raw, str(resolved))]}
        if resolved.is_dir():
            files_found = sorted(resolved.glob("*.json"))
            if not files_found:
                raise ContractError("bundles.verify", "no bundle JSON files found")
            items = []
            for path in files_found:
                raw = path.read_bytes()
                doc = strict_json_loads(raw.decode("utf-8"))
                validate_bundle(doc)
                items.append(_verify_one(_derive_local_name(path, doc), raw, str(path)))
            return {"verified": True, "items": items}
        raise ContractError("bundles.verify", "target must be a file or directory")

    entries = catalog_entries()
    items = []
    registered_files = set()
    for entry in entries:
        registered_files.add(entry["file"])
        raw = _read_resource_bytes(entry["file"])
        items.append(_verify_one(entry["name"], raw, entry["file"], entry))

    # When running against a filesystem benchmarks/ directory, ensure no unregistered bundle JSONs.
    root = _benchmarks_root()
    if isinstance(root, Path):
        bundles_dir = root / "bundles"
        if bundles_dir.is_dir():
            on_disk = {f"bundles/{p.name}" for p in bundles_dir.glob("*.json")}
            unregistered = sorted(on_disk - registered_files)
            if unregistered:
                raise ContractError(
                    "bundles.catalog",
                    f"unregistered bundle files in benchmarks/bundles: {', '.join(unregistered)}",
                )
    return {"verified": True, "default_bundles": list(default_bundles()), "items": items}
