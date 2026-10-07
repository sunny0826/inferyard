"""Bounded no-follow host state reads and stable kernel-lock identities."""

import hashlib
import os
import stat

from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.evidence.storage import atomic_bytes, json_bytes
from inferyard.platforms.identity import PreflightError
from inferyard.platforms.platform_io import is_reparse, open_nofollow

MAX_BYTES = 1024 * 1024


def require(condition, reason="invalid_host_state"):
    if not condition:
        raise PreflightError(reason)


def identity(info):
    return {"device": info.st_dev, "inode": info.st_ino}


def root_identity(path):
    info = path.lstat()
    require(stat.S_ISDIR(info.st_mode) and not is_reparse(info), "unsafe_host_root")
    return identity(info)


def safe_file(info):
    return (
        stat.S_ISREG(info.st_mode)
        and not is_reparse(info)
        and info.st_nlink == 1
        and (
            os.name == "nt" or (info.st_uid == os.getuid() and stat.S_IMODE(info.st_mode) == 0o600)
        )
    )


def exists(path):
    try:
        path.lstat()
        return True
    except FileNotFoundError:
        return False


def read_bytes(path):
    try:
        fd = open_nofollow(path, os.O_RDONLY)
    except FileNotFoundError:
        return None
    except OSError as exc:
        raise PreflightError("host_state_unreadable") from exc
    with os.fdopen(fd, "rb") as stream:
        before = os.fstat(stream.fileno())
        require(safe_file(before), "unsafe_host_state")
        raw = stream.read(MAX_BYTES + 1)
        after = os.fstat(stream.fileno())
        require(len(raw) <= MAX_BYTES, "host_state_size_limit")
        require(
            identity(before) == identity(after) == identity(path.lstat())
            and (before.st_size, before.st_mtime_ns) == (after.st_size, after.st_mtime_ns),
            "host_state_changed_during_read",
        )
    return raw


def decode(raw):
    try:
        value = strict_json_loads(raw.decode("utf-8"))
    except (AttributeError, UnicodeError, ContractError) as exc:
        raise PreflightError("invalid_host_state") from exc
    require(type(value) is dict)
    return value


def validate_state(value):
    require(type(value.get("schema_version")) is int and value["schema_version"] == 1)
    require(type(value.get("dirty")) is bool)
    fields = {
        "dirty_token",
        "run_id",
        "kind",
        "endpoint",
        "server_pid",
        "process_start_ticks",
        "request_id",
    }
    if value["dirty"] or fields & value.keys():
        require(fields <= value.keys())
        for key in fields - {"server_pid", "process_start_ticks"}:
            require(type(value[key]) is str and bool(value[key]))
        for key in ("server_pid", "process_start_ticks"):
            require(type(value[key]) is int and value[key] > 0)
    return value


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def publish_state(path, value, *, overwrite):
    from inferyard.evidence.storage import EvidenceError

    raw = json_bytes(value)
    atomic_bytes(path, raw, overwrite=overwrite)
    if read_bytes(path) != raw:
        raise EvidenceError("host_state_publication_mismatch")
