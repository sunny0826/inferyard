"""Sealed engine-fit artifact layout, manifest and strict offline reads."""

import hashlib
import re
from pathlib import Path
from stat import S_ISREG

from inferyard.analysis.engine_fit_validation import fields, require
from inferyard.contracts.validation import strict_json_loads
from inferyard.errors import ContractError, EvidenceError, UnsupportedFormat
from inferyard.evidence.formats import require_core

RUN_FILES = ("plan.json", "run.json", "requests.json")
COMPARISON_FILES = ("comparison.json",)
DEFINITION = "engine_fit_manifest.v3"
# Retired layouts are only named so they can be rejected after byte checks.
LEGACY_V2_DEFINITION = "engine_fit_manifest.v2"
_LEGACY_V2_RUN_FILES = (*RUN_FILES, "report.html")
_LEGACY_V2_COMPARISON_FILES = (*COMPARISON_FILES, "report.html")


def sha256_hex(content):
    return hashlib.sha256(content).hexdigest()


def require_directory(path):
    path = Path(path)
    require(not path.is_symlink() and path.is_dir(), "unsafe_directory")
    return path


def entry_path(root, name):
    path = root / name
    require(not path.is_symlink(), "unsafe_file")
    require(not path.exists() or path.is_file(), "unsafe_file")
    return path


def read_entry(root, name):
    try:
        return entry_path(root, name).read_bytes()
    except OSError as exc:
        raise EvidenceError("engine_fit_missing_file") from exc


def manifest_present(root):
    try:
        entry = (root / "manifest.json").lstat()
    except FileNotFoundError:
        return False
    except OSError as exc:
        raise EvidenceError("engine_fit_missing_file") from exc
    require(S_ISREG(entry.st_mode), "unsafe_file")
    return True


def decode_json_bytes(content):
    try:
        result = strict_json_loads(content.decode("utf-8"))
        pending = [result]
        while pending:
            item = pending.pop()
            if isinstance(item, str):
                item.encode("utf-8", "strict")
            elif isinstance(item, dict):
                pending.extend(item.keys())
                pending.extend(item.values())
            elif isinstance(item, list):
                pending.extend(item)
        return result
    except (UnicodeError, ContractError) as exc:
        raise EvidenceError("engine_fit_invalid_json") from exc


def manifest_document(blobs, kind, *, definition=DEFINITION):
    return {
        "schema_version": 3,
        "definition": definition,
        "kind": kind,
        "files": {name: sha256_hex(content) for name, content in blobs.items()},
    }


def _hashed_blobs(root, manifest, names):
    fields(manifest["files"], set(names), "manifest_files")
    for digest in manifest["files"].values():
        require(type(digest) is str and re.fullmatch(r"[a-f0-9]{64}", digest), "manifest_hash")
    blobs = {name: read_entry(root, name) for name in names}
    require(
        manifest["files"] == {name: sha256_hex(raw) for name, raw in blobs.items()},
        "hash_mismatch",
    )
    return blobs


def load_manifest(root):
    content = read_entry(root, "manifest.json")
    manifest = decode_json_bytes(content)
    fields(manifest, {"schema_version", "definition", "kind", "files"}, "manifest_fields")
    require(manifest["kind"] in ("run", "comparison"), "manifest_kind")
    definition = manifest["definition"]
    if type(definition) is not str:
        raise EvidenceError("engine_fit_manifest_definition")
    legacy = definition == LEGACY_V2_DEFINITION
    if definition == DEFINITION:
        names = RUN_FILES if manifest["kind"] == "run" else COMPARISON_FILES
    elif legacy:
        names = (
            _LEGACY_V2_RUN_FILES
            if manifest["kind"] == "run"
            else _LEGACY_V2_COMPARISON_FILES
        )
    else:
        raise UnsupportedFormat("engine-fit manifest", definition, (DEFINITION,))
    # Byte checks come first so damage cannot be reported as a retired version.
    blobs = _hashed_blobs(root, manifest, names)
    require_core(manifest, "engine-fit manifest")
    return manifest, {**blobs, "manifest.json": content}
