"""One command's content identities, with explicit reviewed responsibility lists."""

import hashlib
import json
import platform
import sys
import unicodedata
from copy import deepcopy
from functools import cache
from importlib.metadata import PackageNotFoundError, version
from pathlib import Path
from typing import Protocol

from inferyard.errors import ContractError
from inferyard.provenance import tool_source_hash

DEFINITION = "implementation-identity.v1"
ROLES = ("measurement", "scoring", "presentation")
LISTS_ROOT = "data/implementation-files/"
VALIDATION_DEPENDENCIES = (
    "jsonschema",
    "attrs",
    "referencing",
    "rpds-py",
    "jsonschema-specifications",
)
DEPENDENCIES = {
    "measurement": (
        *VALIDATION_DEPENDENCIES,
        "httpx",
        "httpcore",
        "anyio",
        "h11",
        "certifi",
        "idna",
    ),
    "scoring": VALIDATION_DEPENDENCIES,
    "presentation": VALIDATION_DEPENDENCIES,
}


class SourceHasher(Protocol):
    def __call__(self, *, file_hashes: dict[str, str] | None = None) -> str: ...


type JsonScalar = str | int | float | bool | None
type JsonTree = JsonScalar | list[JsonTree] | dict[str, JsonTree]
type IdentityPayload = dict[str, JsonTree]


def digest(value):
    return hashlib.sha256(
        json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()
    ).hexdigest()


def dependency_versions(role):
    names = list(DEPENDENCIES[role])
    if role == "measurement" and sys.platform in ("darwin", "win32"):
        names.append("psutil")
    result = [
        {
            "name": "python",
            "version": platform.python_implementation() + ":" + platform.python_version(),
            "reason": None,
        },
    ]
    if role in ("measurement", "scoring"):
        result.append(
            {"name": "unicodedata", "version": unicodedata.unidata_version, "reason": None}
        )
    for name in sorted(names):
        try:
            value, reason = version(name), None
        except PackageNotFoundError:
            value, reason = None, "installed_dependency_version_unavailable"
        result.append({"name": name, "version": value, "reason": reason})
    return sorted(result, key=lambda item: item["name"])


def _build_identity(root: Path, source_hash: SourceHasher) -> tuple[str, IdentityPayload]:
    """Scan one root; callers own any reuse of the resulting identity."""
    hashes, lists = {}, {}
    for path in sorted(root.rglob("*")):
        if path.suffix not in (".py", ".html", ".json"):
            continue
        raw = path.read_bytes()
        name = path.relative_to(root).as_posix()
        hashes[name] = hashlib.sha256(raw).hexdigest()
        if name.startswith(LISTS_ROOT) and name.endswith(".json"):
            role = name.removeprefix(LISTS_ROOT).removesuffix(".json")
            lists[role] = json.loads(raw)
    source = source_hash(file_hashes=hashes)
    value: IdentityPayload = {"definition": DEFINITION}
    for role in ROLES:
        contents = {
            "files": [{"path": name, "sha256": hashes[name]} for name in lists[role]],
            "dependencies": dependency_versions(role),
        }
        value[role] = {**contents, "sha256": digest(contents)}
    return source, value


@cache
def _default_identity() -> tuple[str, IdentityPayload]:
    """Installed package identity is constant for the life of this process."""
    return _build_identity(Path(__file__).parent, tool_source_hash)


class IdentityContext:
    source: str
    value: IdentityPayload

    def __init__(
        self, *, root: Path | None = None, source_hash: SourceHasher = tool_source_hash
    ) -> None:
        if root is None and source_hash is tool_source_hash:
            source, value = _default_identity()
            self.source, self.value = source, deepcopy(value)
            return
        self.source, self.value = _build_identity(root or Path(__file__).parent, source_hash)


def validate_identity(value):
    from inferyard.contracts.schemas_identity import IMPLEMENTATION_IDENTITY
    from inferyard.contracts.validation import validate_value

    validate_value(value, IMPLEMENTATION_IDENTITY, "implementation_identity")
    for role in ROLES:
        entry = value[role]
        names = [row["path"] for row in entry["files"]]
        deps = [row["name"] for row in entry["dependencies"]]
        if names != sorted(set(names)) or deps != sorted(set(deps)):
            raise ContractError("implementation_identity", "duplicate or unordered entries")
        if any(".." in name.split("/") or name.startswith("/") or "\\" in name for name in names):
            raise ContractError("implementation_identity", "invalid relative path")
        if any((d["version"] is None) != (d["reason"] is not None) for d in entry["dependencies"]):
            raise ContractError("implementation_identity", "invalid missing version reason")
        if entry["sha256"] != digest({k: v for k, v in entry.items() if k != "sha256"}):
            raise ContractError("implementation_identity", "content digest mismatch")


def role_identities(value):
    if value is None:
        return dict.fromkeys(ROLES)
    validate_identity(value)
    return {
        role: None
        if any(d["version"] is None for d in value[role]["dependencies"])
        else value[role]["sha256"]
        for role in ROLES
    }


def role_identity(value, role):
    return role_identities(value)[role]


def execution_matches(previous, current):
    a, b = role_identities(previous), role_identities(current)
    return all(a[role] is not None and a[role] == b[role] for role in ("measurement", "scoring"))
