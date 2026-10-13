"""Deadline-aware Windows file manifest identity. Does not use file_hash."""

from __future__ import annotations

import hashlib

from inferyard.platforms.identity import PreflightError
from inferyard.platforms.windows_lab_api import current_windows_lab_api
from inferyard.platforms.windows_lab_checks import (
    MAX_INT,
    MAX_MANIFEST_BYTES,
    MAX_MANIFEST_ENTRIES,
    path_key,
    positive_int,
    reject_deadline,
    require_sha256,
    windows_file_ancestors,
)

ROLES = frozenset({"model", "engine", "library", "component_ledger", "template"})
_FIELDS = frozenset({"path", "role", "bytes", "sha256"})
READ_CHUNK_BYTES = 1024 * 1024
HASH_BASE_SECONDS = 180
HASH_BYTES_PER_SECOND = 32 * 1024**2


def manifest_hash_seconds(entries: list[dict]) -> int:
    """One bounded manifest budget: fixed overhead plus declared bytes at 32 MiB/s."""
    total = sum(item["bytes"] for item in _parse_entries(entries))
    return HASH_BASE_SECONDS + (total + HASH_BYTES_PER_SECOND - 1) // HASH_BYTES_PER_SECOND


def verify_file_manifest(entries: list[dict], *, deadline: float) -> list[dict]:
    reject_deadline(deadline)
    items = _parse_entries(entries)
    api = current_windows_lab_api()
    verified = [_verify_one(api, item, deadline) for item in items]
    reject_deadline(deadline)
    return verified


def _parse_entries(entries: object) -> list[dict]:
    if type(entries) is not list or not 1 <= len(entries) <= MAX_MANIFEST_ENTRIES:
        raise PreflightError("lab_windows_manifest_invalid")
    parsed = []
    seen: set[str] = set()
    total = 0
    for entry in entries:
        if type(entry) is not dict or set(entry) != _FIELDS:
            raise PreflightError("lab_windows_manifest_invalid")
        role = entry["role"]
        if type(role) is not str or role not in ROLES:
            raise PreflightError("lab_windows_manifest_invalid")
        size = positive_int(entry["bytes"], "lab_windows_manifest_invalid")
        total += size
        if total > MAX_MANIFEST_BYTES:
            raise PreflightError("lab_windows_manifest_invalid")
        ancestors = windows_file_ancestors(entry["path"])
        key = path_key(ancestors[-1])
        if key in seen:
            raise PreflightError("lab_windows_manifest_duplicate")
        seen.add(key)
        parsed.append(
            {
                "path": entry["path"],
                "role": role,
                "bytes": size,
                "sha256": require_sha256(entry["sha256"], "lab_windows_manifest_invalid"),
                "ancestors": ancestors,
            }
        )
    return parsed


def _verify_one(api, item: dict, deadline: float) -> dict:
    try:
        for ancestor in item["ancestors"]:
            reject_deadline(deadline)
            _reject_reparse(api, ancestor)
            reject_deadline(deadline)
        leaf = item["ancestors"][-1]
        before = _require_stamp(api.path_stamp(leaf))
        reject_deadline(deadline)
        if type(before[3]) is not int or before[3] != item["bytes"]:
            raise PreflightError("lab_windows_file_identity_mismatch")
        handle = api.open_read(leaf)
        try:
            if _require_stamp(api.descriptor_stamp(handle)) != before:
                raise PreflightError("lab_windows_file_changed")
            reject_deadline(deadline)
            digest = _digest(api, handle, item["bytes"], deadline)
            if _require_stamp(api.descriptor_stamp(handle)) != before:
                raise PreflightError("lab_windows_file_changed")
            reject_deadline(deadline)
        finally:
            api.close_file(handle)
        reject_deadline(deadline)
        if _require_stamp(api.path_stamp(leaf)) != before:
            raise PreflightError("lab_windows_file_changed")
        reject_deadline(deadline)
    except OSError as exc:
        raise PreflightError("lab_windows_file_unreadable") from exc
    if digest != item["sha256"]:
        raise PreflightError("lab_windows_hash_mismatch")
    reject_deadline(deadline)
    return {
        "path": item["path"],
        "role": item["role"],
        "bytes": item["bytes"],
        "sha256": item["sha256"],
        "native_stamp": _native_stamp(before),
    }


def _reject_reparse(api, path: str) -> None:
    try:
        api.reject_reparse(path)
    except OSError as exc:
        if str(exc) == "reparse_point_rejected":
            raise PreflightError("lab_windows_reparse_rejected") from exc
        raise


def _digest(api, handle, size: int, deadline: float) -> str:
    hasher = hashlib.sha256()
    remaining = size
    while remaining:
        reject_deadline(deadline)
        chunk = api.read_file(handle, min(READ_CHUNK_BYTES, remaining))
        reject_deadline(deadline)
        if type(chunk) is not bytes or not chunk or len(chunk) > remaining:
            raise PreflightError("lab_windows_file_changed")
        hasher.update(chunk)
        remaining -= len(chunk)
    extra = api.read_file(handle, 1)
    reject_deadline(deadline)
    if type(extra) is not bytes or extra:
        raise PreflightError("lab_windows_file_changed")
    return hasher.hexdigest()


def _require_stamp(value):
    if type(value) is not tuple or len(value) != 7:
        raise PreflightError("lab_windows_file_unreadable")
    return value


def _native_stamp(stamp) -> list:
    volume, identifier, attributes, size, creation, write, change = stamp
    if type(volume) is not int or not 0 <= volume <= 2**64 - 1:
        raise PreflightError("lab_windows_file_unreadable")
    if type(identifier) is not bytes or len(identifier) != 16:
        raise PreflightError("lab_windows_file_unreadable")
    if type(attributes) is not int or not 0 <= attributes <= 0xFFFFFFFF:
        raise PreflightError("lab_windows_file_unreadable")
    if type(size) is not int or not 0 < size <= MAX_INT:
        raise PreflightError("lab_windows_file_unreadable")
    for value in (creation, write, change):
        if type(value) is not int or not -(2**63) <= value <= MAX_INT:
            raise PreflightError("lab_windows_file_unreadable")
    return [volume, identifier.hex(), attributes, size, creation, write, change]
