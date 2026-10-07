"""Pure checks for Windows lab identity. This module does not load native APIs."""

from __future__ import annotations

import hashlib
import ipaddress
import time
from urllib.parse import urlsplit

from inferyard.platforms.identity import PreflightError

MAX_INT = 2**63 - 1
MAX_DWORD = 0xFFFFFFFF
MAX_PATH_CHARS = 32767
MAX_ARGV = 1024
MAX_ARGV_BYTES = 128 * 1024
MAX_MANIFEST_ENTRIES = 256
MAX_MANIFEST_BYTES = 64 * 1024**3
_SHA = "0123456789abcdef"


def reject_deadline(deadline: float) -> None:
    if type(deadline) is not float or not _finite(deadline):
        raise PreflightError("lab_windows_deadline_invalid")
    if time.monotonic() >= deadline:
        raise PreflightError("lab_windows_deadline_exceeded")


def positive_int(value: object, code: str, *, limit: int = MAX_INT) -> int:
    if type(value) is not int or not 0 < value <= limit:
        raise PreflightError(code)
    return value


def require_pid(value: object) -> int:
    return positive_int(value, "lab_windows_invalid_pid", limit=MAX_DWORD)


def require_sha256(value: object, code: str) -> str:
    if (
        type(value) is not str
        or len(value) != 64
        or any(character not in _SHA for character in value)
    ):
        raise PreflightError(code)
    return value


def parse_origin(origin: object) -> tuple[str, int]:
    if type(origin) is not str or not origin or any(character.isspace() for character in origin):
        raise PreflightError("lab_windows_origin_invalid")
    if not origin.startswith(("http://", "https://")) or any(mark in origin for mark in "@?#"):
        raise PreflightError("lab_windows_origin_invalid")
    try:
        parts = urlsplit(origin)
        port = parts.port
    except ValueError as exc:
        raise PreflightError("lab_windows_origin_invalid") from exc
    host = parts.hostname
    if (
        parts.scheme not in {"http", "https"}
        or parts.path not in {""}
        or parts.username is not None
        or parts.password is not None
        or parts.query
        or parts.fragment
        or host is None
        or "%" in host
        or type(port) is not int
        or not 1 <= port <= 65535
    ):
        raise PreflightError("lab_windows_origin_invalid")
    try:
        address = ipaddress.ip_address(host)
    except ValueError as exc:
        raise PreflightError("lab_windows_origin_invalid") from exc
    mapped = getattr(address, "ipv4_mapped", None)
    if (
        not address.is_loopback
        or address.is_unspecified
        or mapped is not None
        or str(address) != host
    ):
        raise PreflightError("lab_windows_origin_invalid")
    return str(address), port


def same_account(current: object, owner: object) -> None:
    if (
        type(current) is not str
        or type(owner) is not str
        or not current
        or not owner
        or "\0" in current
        or "\0" in owner
    ):
        raise PreflightError("lab_windows_identity_incomplete")
    if current.casefold() != owner.casefold():
        raise PreflightError("lab_windows_account_mismatch")


def argv_digest(arguments: object) -> str:
    if type(arguments) is not list or not arguments or len(arguments) > MAX_ARGV:
        raise PreflightError("lab_windows_identity_incomplete")
    encoded: list[bytes] = []
    for argument in arguments:
        if type(argument) is not str or "\0" in argument:
            raise PreflightError("lab_windows_identity_incomplete")
        try:
            encoded.append(argument.encode("utf-8"))
        except UnicodeEncodeError as exc:
            raise PreflightError("lab_windows_identity_incomplete") from exc
    raw = b"\0".join(encoded) + b"\0"
    if len(raw) > MAX_ARGV_BYTES:
        raise PreflightError("lab_windows_identity_incomplete")
    return hashlib.sha256(raw).hexdigest()


def cwd_digest(path: object) -> str:
    if type(path) is not str or "/" in path or not _directory_path(path):
        raise PreflightError("lab_windows_identity_incomplete")
    try:
        encoded = path.encode("utf-8")
    except UnicodeEncodeError as exc:
        raise PreflightError("lab_windows_identity_incomplete") from exc
    return hashlib.sha256(encoded).hexdigest()


def same_windows_path(left: object, right: object) -> bool:
    if type(left) is not str or type(right) is not str:
        return False
    return _path_key(left) == _path_key(right)


def path_key(path: str) -> str:
    return _path_key(path)


def windows_file_ancestors(path: object) -> list[str]:
    text = _file_text(path)
    if text.startswith("\\\\"):
        parts = text[2:].split("\\")
        if len(parts) < 3 or not all(_component_ok(part) for part in parts):
            raise PreflightError("lab_windows_manifest_invalid")
        current = "\\\\" + parts[0] + "\\" + parts[1]
        ancestors = [current]
        for part in parts[2:]:
            current += "\\" + part
            ancestors.append(current)
        return ancestors
    tail = text[3:]
    if not tail or tail.endswith("\\") or not all(_component_ok(part) for part in tail.split("\\")):
        raise PreflightError("lab_windows_manifest_invalid")
    ancestors = [text[:3]]
    current = text[:2]
    for part in tail.split("\\"):
        current += "\\" + part
        ancestors.append(current)
    return ancestors


def listener_identity(address: str, port: int, pid: int, rows: object) -> str:
    if type(rows) is not list:
        raise PreflightError("lab_windows_identity_incomplete")
    origin = ipaddress.ip_address(address)
    candidates: list[tuple[ipaddress.IPv4Address | ipaddress.IPv6Address, object]] = []
    for row in rows:
        parsed = _listener_row(row, port)
        if parsed is None:
            continue
        listener, row_pid = parsed
        if _could_accept(listener, origin):
            candidates.append((listener, row_pid))
    if not candidates:
        raise PreflightError("lab_windows_listener_mismatch")
    listener, row_pid = candidates[0]
    if (
        len(candidates) != 1
        or listener != origin
        or listener.is_unspecified
        or listener.version != origin.version
    ):
        raise PreflightError("lab_windows_listener_ambiguous")
    if type(row_pid) is not int:
        raise PreflightError("lab_windows_identity_incomplete")
    if row_pid != pid:
        raise PreflightError("lab_windows_listener_mismatch")
    return f"windows:tcp:{address}:{port}:pid:{pid}"


def _finite(value: float) -> bool:
    return value == value and value not in {float("inf"), float("-inf")}


def _path_key(path: str) -> str:
    return path.replace("/", "\\").casefold()


def _component_ok(part: str) -> bool:
    if not part or part in {".", ".."} or part.endswith((".", " ")):
        return False
    return not any(character in part for character in '<>:"|?*')


def _file_text(path: object) -> str:
    if type(path) is not str or not path or "\0" in path or len(path) > MAX_PATH_CHARS:
        raise PreflightError("lab_windows_manifest_invalid")
    text = path.replace("/", "\\")
    if text.startswith(("\\\\?\\", "\\\\.\\")):
        raise PreflightError("lab_windows_manifest_invalid")
    drive = len(text) >= 3 and text[0].isascii() and text[0].isalpha() and text[1:3] == ":\\"
    if not text.startswith("\\\\") and not drive:
        raise PreflightError("lab_windows_manifest_invalid")
    return text


def _directory_path(path: str) -> bool:
    if (
        not path
        or "\0" in path
        or len(path) > MAX_PATH_CHARS
        or path.startswith(("\\\\?\\", "\\\\.\\"))
    ):
        return False
    if path.startswith("\\\\"):
        parts = path[2:].split("\\")
        return (
            len(parts) >= 2
            and not path.endswith("\\")
            and all(_component_ok(part) for part in parts)
        )
    if len(path) >= 3 and path[0].isascii() and path[0].isalpha() and path[1:3] == ":\\":
        tail = path[3:]
        return tail == "" or (
            not tail.endswith("\\") and all(_component_ok(part) for part in tail.split("\\"))
        )
    return False


def _listener_row(row: object, port: int):
    if type(row) is not tuple or len(row) != 3:
        raise PreflightError("lab_windows_identity_incomplete")
    ip_text, row_port, row_pid = row
    if type(row_port) is not int or not 1 <= row_port <= 65535:
        raise PreflightError("lab_windows_identity_incomplete")
    if row_port != port:
        return None
    if type(ip_text) is not str:
        raise PreflightError("lab_windows_identity_incomplete")
    try:
        return ipaddress.ip_address(ip_text), row_pid
    except ValueError as exc:
        raise PreflightError("lab_windows_identity_incomplete") from exc


def _could_accept(listener, origin) -> bool:
    if listener.is_unspecified:
        return origin.version == 4 or listener.version == 6
    mapped = getattr(listener, "ipv4_mapped", None)
    if mapped is not None and origin.version == 4:
        return True
    return listener == origin
