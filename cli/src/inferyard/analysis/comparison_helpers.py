"""Shared explicit comparison condition helpers."""

from __future__ import annotations

import hashlib

from inferyard.contracts.validation import json_bytes
from inferyard.errors import ContractError

_LEGACY_IDENTITY_ROLES = ("measurement", "scoring", "presentation")


def _hex_digest(value) -> bool:
    return (
        type(value) is str
        and len(value) == 64
        and all(ch in "0123456789abcdef" for ch in value)
    )


def legacy_role_identities(value):
    """Validate archived implementation_identity records for v5/phase2.v3 replay only."""
    if value is None:
        return dict.fromkeys(_LEGACY_IDENTITY_ROLES)
    if (
        type(value) is not dict
        or set(value) != {"definition", *_LEGACY_IDENTITY_ROLES}
        or value["definition"] != "implementation-identity.v1"
    ):
        raise ContractError("implementation_identity", "invalid identity structure")
    out = {}
    for role in _LEGACY_IDENTITY_ROLES:
        entry = value[role]
        if (
            type(entry) is not dict
            or set(entry) != {"files", "dependencies", "sha256"}
            or type(entry["files"]) is not list
            or not entry["files"]
            or type(entry["dependencies"]) is not list
            or not entry["dependencies"]
            or not _hex_digest(entry["sha256"])
        ):
            raise ContractError("implementation_identity", "invalid role entry")
        names = []
        for row in entry["files"]:
            if (
                type(row) is not dict
                or set(row) != {"path", "sha256"}
                or type(row["path"]) is not str
                or not row["path"]
                or not _hex_digest(row["sha256"])
            ):
                raise ContractError("implementation_identity", "invalid file entry")
            names.append(row["path"])
        deps = []
        missing_version = False
        for dep in entry["dependencies"]:
            if (
                type(dep) is not dict
                or set(dep) != {"name", "version", "reason"}
                or type(dep["name"]) is not str
                or not dep["name"]
                or (dep["version"] is not None and (type(dep["version"]) is not str or not dep["version"]))
                or (dep["reason"] is not None and (type(dep["reason"]) is not str or not dep["reason"]))
                or (dep["version"] is None) != (dep["reason"] is not None)
            ):
                raise ContractError("implementation_identity", "invalid dependency entry")
            deps.append(dep["name"])
            if dep["version"] is None:
                missing_version = True
        if names != sorted(set(names)) or deps != sorted(set(deps)):
            raise ContractError("implementation_identity", "duplicate or unordered entries")
        if any(".." in name.split("/") or name.startswith("/") or "\\" in name for name in names):
            raise ContractError("implementation_identity", "invalid relative path")
        expected = hashlib.sha256(
            json_bytes({k: v for k, v in entry.items() if k != "sha256"})
        ).hexdigest()
        if entry["sha256"] != expected:
            raise ContractError("implementation_identity", "content digest mismatch")
        out[role] = None if missing_version else entry["sha256"]
    return out


def _get(data, path):
    value = data
    for key in path.split("."):
        if not isinstance(value, dict) or key not in value:
            return None
        value = value[key]
    return value


def _known(value):
    if value is None or value == "unknown" or value == "":
        return False
    if isinstance(value, dict):
        return bool(value) and all(_known(v) for v in value.values())
    if isinstance(value, list):
        return all(_known(v) for v in value)
    return True


def comparable_arguments(args):
    location = {"-m", "--model", "--host", "--port", "-a", "--alias"}
    result = []
    skip = False
    for argument in args:
        if skip:
            skip = False
        elif argument in location:
            skip = True
        elif argument.split("=", 1)[0] in location:
            continue
        else:
            result.append(argument)
    return result
