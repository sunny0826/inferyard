"""Frozen cross-process protocol constants for InferYard remote node control and tunnels.

Mirrors ``studio/internal/remote/api.go`` so Studio (Go) and CLI Node Agent (Python)
stay byte-for-byte compatible across parallel implementation lanes.
"""

from __future__ import annotations

import json
import re
from typing import Any

PROTOCOL_VERSION = 1

HANDSHAKE_PAIR_LINE = "INFERYARD_PAIR 1\n"
HANDSHAKE_CONTROL_LINE = "INFERYARD_CONTROL 1\n"
HANDSHAKE_TUNNEL_PREFIX = "INFERYARD_TUNNEL 1 "

MAX_CONTROL_LINE_BYTES = 16 * 1024 * 1024  # 16 MiB

# Control-plane RPC methods (Studio -> Agent).
METHOD_NODE_DEVICE_CHECK = "node.device_check"
METHOD_NODE_ASSETS_SCAN = "node.assets_scan"
METHOD_NODE_ENGINE_INSTALL = "node.engine_install"

METHOD_SERVICE_LIST = "service.list"
METHOD_SERVICE_START = "service.start"
METHOD_SERVICE_STOP = "service.stop"
METHOD_SERVICE_LOGS = "service.logs"

METHOD_JOB_LIST = "job.list"
METHOD_JOB_START = "job.start"
METHOD_JOB_CANCEL = "job.cancel"
METHOD_JOB_PROGRESS = "job.progress"

METHOD_REPORT_LIST = "report.list"
METHOD_REPORT_EXPORT = "report.export"

METHOD_WORKSPACE_CLEAN = "workspace.clean"

# Control-plane one-way events.
EVENT_HELLO = "hello"
EVENT_PING = "ping"
EVENT_PONG = "pong"
EVENT_OPEN_TUNNEL = "open_tunnel"
EVENT_SERVICE_UPDATED = "service.updated"
EVENT_SERVICE_EXITED = "service.exited"
EVENT_JOB_UPDATED = "job.updated"
EVENT_JOB_PROGRESS = "job.progress"
EVENT_DEVICE_UPDATED = "device.updated"

# Explicit option-based pairing decisions (never boolean).
PAIRING_OPTION_APPROVE = "approve_pair"
PAIRING_OPTION_REJECT = "reject_pair"
PAIRING_OPTIONS = (PAIRING_OPTION_APPROVE, PAIRING_OPTION_REJECT)

# Cooperative job cancellation marker filename inside jobs/<job_id>/.
CANCEL_MARKER_FILENAME = "cancel.json"

_STREAM_ID_RE = re.compile(r"^[A-Za-z0-9._-]{1,128}$")
_PAIR_CODE_RE = re.compile(r"^[0-9]{6}$")


def format_tunnel_handshake(stream_id: str) -> str:
    """Return the validated wire handshake line for an on-demand TUNNEL stream."""
    if not _STREAM_ID_RE.match(stream_id):
        raise ValueError("invalid tunnel stream_id")
    return f"{HANDSHAKE_TUNNEL_PREFIX}{stream_id}\n"


def normalize_pair_code(raw: str) -> str:
    """Strip dashes/spaces and validate a 6-digit one-time pairing code."""
    cleaned = re.sub(r"[\s-]+", "", raw.strip())
    if not _PAIR_CODE_RE.match(cleaned):
        raise ValueError("pairing code must be 6 digits")
    return cleaned


def encode_ndjson_frame(payload: dict[str, Any]) -> bytes:
    """Serialize one control-plane NDJSON frame as UTF-8 bytes with trailing newline."""
    encoded = (json.dumps(payload, ensure_ascii=False, separators=(",", ":")) + "\n").encode(
        "utf-8"
    )
    if len(encoded) > MAX_CONTROL_LINE_BYTES:
        raise ValueError("control frame exceeds maximum allowed size")
    return encoded


def decode_ndjson_frame(line: bytes) -> dict[str, Any]:
    """Decode one UTF-8 NDJSON control frame and enforce JSON object structure."""
    if len(line) > MAX_CONTROL_LINE_BYTES:
        raise ValueError("control frame exceeds maximum allowed size")
    parsed = json.loads(line.decode("utf-8"))
    if not isinstance(parsed, dict):
        raise ValueError("control frame must be a JSON object")
    return parsed
