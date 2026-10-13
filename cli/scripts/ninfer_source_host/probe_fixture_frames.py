"""诊断夹具的身份帧。只做严格 JSON，不启动子进程，不调用 WinAPI。"""

from __future__ import annotations

import json

from .file_io_codec import PROTOCOL, loads_strict
from .probe_io_limits import EXPERIMENT_STAGE, FRAME_FIELDS, LIBRARY_STAGE, MODES, READY_FIELDS

_IDENTITY = ("protocol", "host_id", "execution_sha256", "scripts_sha256", "stage", "job_id")


def require_mode(mode):
    if type(mode) is not str or mode not in MODES:
        raise ValueError("fixture_mode")
    return mode


def require_request(mode, request):
    from .file_io import validate_io_request

    require_mode(mode)
    if type(request) is not dict:
        raise ValueError("fixture_request")
    item = validate_io_request(request)
    if item["protocol"] != PROTOCOL or item["stage"] != LIBRARY_STAGE:
        raise ValueError("fixture_identity")
    return item


def ready_frame(mode, request):
    return _frame(READY_FIELDS, mode, request, "ready", None, None)


def result_frame(mode, request, status, result, *, injected):
    return _frame(FRAME_FIELDS, mode, request, status, result, injected)


_SUCCESS = frozenset({"ok", "returned", "creation_rejected", "job_terminated"})


def completion_code(frame):
    """临时 ok 不能单独成立。返回码还要看主错、收尾错和最终状态。"""
    result = frame.get("result")
    if type(result) is not dict:
        return 2
    if result.get("primary_error") is not None:
        return 2
    cleanup = result.get("cleanup_errors")
    if type(cleanup) is list and cleanup:
        return 2
    if frame.get("status") not in _SUCCESS:
        return 2
    return 0


def loads_frame(raw):
    if type(raw) is bytearray:
        raw = bytes(raw)
    if type(raw) is not bytes:
        raise ValueError("fixture_frame")
    value = loads_strict(raw.decode("utf-8"))
    if set(value) != set(READY_FIELDS) and set(value) != set(FRAME_FIELDS):
        raise ValueError("fixture_frame")
    return value


def _frame(fields, mode, request, status, result, injected):
    payload = {
        "experiment_stage": EXPERIMENT_STAGE,
        "mode": mode,
        "status": status,
    }
    for name in _IDENTITY:
        payload[name] = request[name]
    if injected is not None:
        payload["injected"] = injected
        payload["result"] = result
    ordered = {name: payload[name] for name in fields}
    if set(ordered) != set(fields):
        raise RuntimeError("fixture_fields")
    encoded = json.dumps(ordered, separators=(",", ":"), default=_json).encode("utf-8")
    return encoded


def _json(value):
    raise TypeError("fixture_json")
