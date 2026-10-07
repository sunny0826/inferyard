"""Shared strict wire primitives for the frozen lab protocol modules (T0-A, ADR030).

Both ``lab_observation`` and ``lab_generation`` build on these helpers so duplicate-key,
non-finite number, boolean-as-count, and UTF-8 rejection stay identical across modules.
This batch ships parsing and validation only; nothing here performs I/O.
"""

from __future__ import annotations

from typing import Any

from inferyard.contracts.validation import ContractError, strict_json_loads

PROTOCOL = "lab_observation.v1"
MAX_RESPONSE_BYTES = 2 * 1024**2
MAX_SSE_FRAME_BYTES = 256 * 1024
MAX_INT = 2**63 - 1
ACTIVE_PHASES = (
    "accepted_waiting",
    "preparing",
    "prefilling",
    "decoding",
    "output_draining",
    "releasing",
)
PHASE_RANK = {phase: rank for rank, phase in enumerate(ACTIVE_PHASES)}
RANK_RELEASED = len(ACTIVE_PHASES)
ENGINES = ("ninfer", "kvmem")
MODEL_KINDS = {"ninfer": "ninfer", "kvmem": "gguf"}

_HEX_DIGITS = frozenset("0123456789abcdef")


class LabProtocolError(RuntimeError):
    """Stable reason code only; never echoes response bytes, paths, or credentials."""

    def __init__(self, reason: str):
        super().__init__(reason)
        self.reason = reason


def decode_utf8(raw: bytes | bytearray, reason: str) -> str:
    if not isinstance(raw, (bytes, bytearray)):
        raise LabProtocolError(reason)
    if len(raw) > MAX_RESPONSE_BYTES:
        raise LabProtocolError(reason)
    try:
        return bytes(raw).decode("utf-8", "strict")
    except UnicodeDecodeError as exc:
        raise LabProtocolError(reason) from exc


def parse_json(text: str, reason: str) -> Any:
    try:
        return strict_json_loads(text)
    except (ContractError, ValueError, RecursionError) as exc:
        raise LabProtocolError(reason) from exc


def load_strict(raw: bytes | bytearray, reason: str) -> Any:
    return parse_json(decode_utf8(raw, reason), reason)


def require_object(value: Any, reason: str) -> dict:
    if type(value) is not dict:
        raise LabProtocolError(reason)
    return value


def require_exact_keys(obj: dict, keys: set | frozenset, reason: str) -> None:
    if set(obj) != set(keys):
        raise LabProtocolError(reason)


def require_bool(value: Any, reason: str) -> bool:
    if type(value) is not bool:
        raise LabProtocolError(reason)
    return value


def require_str_enum(value: Any, allowed: tuple | frozenset, reason: str) -> str:
    if type(value) is not str or value not in allowed:
        raise LabProtocolError(reason)
    return value


def require_int(value: Any, reason: str, *, minimum: int = 0) -> int:
    if type(value) is not int or not minimum <= value <= MAX_INT:
        raise LabProtocolError(reason)
    return value


def _require_hex(value: Any, length: int, reason: str) -> str:
    if (
        type(value) is not str
        or len(value) != length
        or any(char not in _HEX_DIGITS for char in value)
    ):
        raise LabProtocolError(reason)
    return value


def require_id32(value: Any, reason: str) -> str:
    return _require_hex(value, 32, reason)


def require_sha256(value: Any, reason: str) -> str:
    return _require_hex(value, 64, reason)
