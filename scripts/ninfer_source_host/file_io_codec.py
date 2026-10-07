"""文件 worker 的请求校验。无文件系统、时钟或 WinAPI。"""

from __future__ import annotations

import base64
import hashlib
import json
import re

PROTOCOL = "ninfer-source-host/1"
STAGE = 45
BODY_LIMIT = 8192
HASH_LIMIT = 40 * 1024 * 1024
DECODED_ARG_LIMIT = 12288
RESULT_LIMIT = 24576
REQUEST_FIELDS = (
    "protocol",
    "host_id",
    "execution_sha256",
    "scripts_sha256",
    "stage",
    "job_id",
    "task_end_ticks",
    "total_end_ticks",
    "frequency",
    "operation",
    "path",
    "expected_bytes",
    "expected_sha256",
    "data_b64",
)
RESULT_FIELDS = (
    "protocol",
    "host_id",
    "execution_sha256",
    "scripts_sha256",
    "stage",
    "job_id",
    "operation",
    "bytes",
    "sha256",
    "data_b64",
    "primary_error",
    "cleanup_errors",
)
IO_CODES = frozenset(
    {"io_request", "io_deadline", "io_identity", "io_path", "io_failure", "io_cleanup"}
)
_UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
_SHA = re.compile(r"[0-9a-f]{64}")
_MARKS = ("\u00b9", "\u00b2", "\u00b3")
_RESERVED = frozenset(
    {"CON", "PRN", "AUX", "NUL"}
    | {f"COM{index}" for index in range(10)}
    | {f"LPT{index}" for index in range(10)}
    | {f"COM{mark}" for mark in _MARKS}
    | {f"LPT{mark}" for mark in _MARKS}
)
_CONTROLS = frozenset(chr(code) for code in range(1, 32))
_ILLEGAL = '<>"|?*'


class IoError(Exception):
    def __init__(self, code):
        if type(code) is not str or code not in IO_CODES:
            raise ValueError(f"unknown io error code: {code!r}")
        super().__init__(code)
        self.code = code


def loads_strict(text):
    if type(text) is not str:
        raise IoError("io_request")

    def pairs(items):
        seen = set()
        out = {}
        for key, value in items:
            if key in seen:
                raise IoError("io_request")
            seen.add(key)
            out[key] = value
        return out

    def reject(_token):
        raise IoError("io_request")

    try:
        value = json.loads(text, object_pairs_hook=pairs, parse_constant=reject)
    except IoError:
        raise
    except json.JSONDecodeError, ValueError, TypeError:
        raise IoError("io_request") from None
    if type(value) is not dict:
        raise IoError("io_request")
    return value


def lexical_ancestors(path):
    if type(path) is not str:
        raise IoError("io_request")
    if not path or "\0" in path or "/" in path:
        raise IoError("io_path")
    if path.startswith("\\\\?\\") or path.startswith("\\\\.\\"):
        raise IoError("io_path")
    if len(path) >= 3 and path[0].isalpha() and path[1:3] == ":\\":
        return _drive_ancestors(path)
    if path.startswith("\\\\"):
        return _unc_ancestors(path)
    raise IoError("io_path")


def validate_io_request(value: dict) -> dict:
    if type(value) is not dict or set(value) != set(REQUEST_FIELDS):
        raise IoError("io_request")
    item = {key: value[key] for key in REQUEST_FIELDS}
    _identity(item)
    _ticks(item)
    _operation(item)
    return item


def decode_write(item):
    data = item["data_b64"]
    if type(data) is not str or len(data) > _encoded_limit(BODY_LIMIT):
        raise IoError("io_request")
    try:
        raw = base64.b64decode(data, validate=True)
    except ValueError, TypeError:
        raise IoError("io_request") from None
    if len(raw) != item["expected_bytes"]:
        raise IoError("io_request")
    if hashlib.sha256(raw).hexdigest() != item["expected_sha256"]:
        raise IoError("io_request")
    return raw


def _encoded_limit(size):
    return (size + 2) // 3 * 4


def _identity(item):
    protocol = item["protocol"]
    if type(protocol) is not str:
        raise IoError("io_request")
    if protocol != PROTOCOL:
        raise IoError("io_identity")
    for name in ("host_id", "job_id"):
        if type(item[name]) is not str:
            raise IoError("io_request")
        if _UUID.fullmatch(item[name]) is None:
            raise IoError("io_identity")
    for name in ("execution_sha256", "scripts_sha256"):
        _sha(item[name], identity=True)
    stage = item["stage"]
    if type(stage) is not int:
        raise IoError("io_request")
    if stage != STAGE:
        raise IoError("io_identity")


def _ticks(item):
    task = item["task_end_ticks"]
    total = item["total_end_ticks"]
    frequency = item["frequency"]
    if type(task) is not int or type(total) is not int or task < 0 or total < 0:
        raise IoError("io_request")
    if total <= task:
        raise IoError("io_request")
    if type(frequency) is not int or frequency <= 0:
        raise IoError("io_request")


def _operation(item):
    operation = item["operation"]
    if type(operation) is not str or operation not in {"read", "hash", "write_new"}:
        raise IoError("io_request")
    lexical_ancestors(item["path"])
    size = item["expected_bytes"]
    if type(size) is not int or size < 0 or size > HASH_LIMIT:
        raise IoError("io_request")
    _sha(item["expected_sha256"], identity=False)
    if operation in {"read", "hash"}:
        if item["data_b64"] is not None:
            raise IoError("io_request")
        if operation == "read" and size > BODY_LIMIT:
            raise IoError("io_request")
        return
    if size > BODY_LIMIT:
        raise IoError("io_request")
    decode_write(item)


def _sha(value, *, identity):
    if type(value) is not str:
        raise IoError("io_request")
    if _SHA.fullmatch(value) is None:
        raise IoError("io_identity" if identity else "io_request")


def _drive_ancestors(path):
    found = [path[:3]]
    for part in _tail(path[3:]):
        base = found[-1]
        found.append(base + part if base.endswith("\\") else base + "\\" + part)
    return tuple(found)


def _unc_ancestors(path):
    parts = path[2:].split("\\")
    if len(parts) < 2:
        raise IoError("io_path")
    for part in parts:
        _component(part)
    current = "\\\\" + parts[0] + "\\" + parts[1]
    found = [current]
    for part in parts[2:]:
        current = current + "\\" + part
        found.append(current)
    return tuple(found)


def _tail(rest):
    if rest == "":
        return []
    parts = rest.split("\\")
    for part in parts:
        _component(part)
    return parts


def _component(part):
    if part in {"", ".", ".."} or part.endswith((" ", ".")) or ":" in part:
        raise IoError("io_path")
    if any(char in _ILLEGAL or char in _CONTROLS for char in part):
        raise IoError("io_path")
    if part.split(".", 1)[0].upper() in _RESERVED:
        raise IoError("io_path")
