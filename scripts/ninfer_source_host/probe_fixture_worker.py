"""诊断 worker 的四种模式。真实 WinAPI 只在 Windows 且没有注入时构造。"""

from __future__ import annotations

from .file_io import execute_io
from .probe_fixture_clock import ClockError
from .probe_fixture_custody import DeadlineStop, PipeHold, _Pipe
from .probe_fixture_descendant import fixed_command, try_descendant
from .probe_fixture_files import FileGate
from .probe_fixture_frames import completion_code, loads_frame, ready_frame, result_frame
from .probe_fixture_publish import Accepted, flush_exact, publish_bytes, write_exact
from .probe_io_limits import FILE_LIMIT, FILE_ROOT, OUTPUT_BODY, OUTPUT_PREFIX, STREAM_LIMIT


def run_worker(mode, request, io_api, native_api, *, stdout, gate):
    """使用入口已经绑定的同一个 gate。这里不再新建时钟或预算。"""
    if mode == "file_io":
        return _file_io(request, io_api, native_api, gate, stdout)
    if mode == "blocked_read":
        return _blocked(request, native_api, stdout, gate)
    if mode == "output":
        return _output(stdout, gate)
    if mode == "descendant":
        return _descendant(request, native_api, gate, stdout)
    raise ValueError("fixture_mode")


def _file_io(request, io_api, native_api, gate, stdout):
    injected = io_api is not None or native_api is not None
    if io_api is None:
        from .probe_io import ProbeFileApi

        io_api = ProbeFileApi(FILE_ROOT, native_api)
    if request["expected_bytes"] > FILE_LIMIT:
        raise ValueError("fixture_file_limit")
    from .probe_io import ProbeFileApi

    if isinstance(io_api, ProbeFileApi):
        io_api.bind_gate(gate)
    proxy = FileGate(io_api, gate)
    try:
        result = execute_io(request, io_api=proxy, now_ticks=_io_now(gate))
    except ClockError as exc:
        gate.note_exception(exc)
        proxy.attach(exc)
        if proxy.live():
            raise
        result = _empty_io(request)
    except BaseException as exc:
        proxy.attach(exc)
        raise
    if gate.sticky and isinstance(gate.cause, BaseException):
        proxy.attach(gate.cause)
        if result is not None and getattr(gate.cause, "io_result", None) is None:
            gate.cause.io_result = result
        raise gate.cause
    if proxy.live():
        raise proxy.hold(result)
    gate.after()
    if gate.veto or gate.blocked():
        result = _expire(result)
    status = "ok" if _clean(result) and not gate.veto and not gate.blocked() else "error"
    frame = result_frame("file_io", request, status, result, injected=injected)
    return _commit(stdout, gate, frame, request, "file_io", injected, result)


def _blocked(request, native, stdout, gate):
    native = _native(native)
    read_handle, write_handle = native.create_pipe()
    owned = _Pipe(native, read_handle, write_handle)
    primary = None
    chunk = None
    try:
        if not gate.work_open():
            primary = DeadlineStop("after_create")
        else:
            ready = ready_frame("blocked_read", request) + b"\n"
            _ready(native, stdout, ready, gate)
            chunk = native.read(read_handle, 1)
            if not gate.work_open():
                raise DeadlineStop("after_read")
    except BaseException as exc:
        _attach_pipe(exc, native, owned)
        if isinstance(exc, (KeyboardInterrupt, SystemExit)) or gate.sticky:
            raise
        primary = exc
        gate.note_exception(exc)
    if gate.blocked():
        _raise_pipe(native, owned, primary, chunk, "deadline")
    try:
        owned.close_with_gate(gate)
    except BaseException as exc:
        _attach_pipe(exc, native, owned)
        raise
    if not owned.fully_released() or gate.blocked():
        observed = "close_unknown" if owned.errors else "deadline"
        _raise_pipe(native, owned, primary, chunk, observed)
    injected = getattr(native, "injected", False)
    if primary is not None or gate.veto:
        body = _blocked_error(primary, chunk, owned)
        frame = result_frame("blocked_read", request, "error", body, injected=injected)
        return _commit(stdout, gate, frame, request, "blocked_read", injected, body)
    body = {
        "api": "ReadFile",
        "pipe": "anonymous",
        "writer_retained": True,
        "bytes": None if type(chunk) is not bytes else len(chunk),
        "completed": type(chunk) is bytes,
        "blocking_proved": False,
        "primary_error": None,
        "cleanup_errors": [],
    }
    frame = result_frame("blocked_read", request, "returned", body, injected=injected)
    return _commit(stdout, gate, frame, request, "blocked_read", injected, body)


def _output(stdout, gate):
    payload = _payload()
    if not gate.work_open():
        return 2, Accepted(payload, 0, False)
    accepted = publish_bytes(stdout, gate, payload, cleanup=False)
    if not accepted.flushed or gate.veto or gate.blocked():
        return 2, accepted
    return 0, accepted


def _descendant(request, native, gate, stdout):
    native = _native(native)
    injected = getattr(native, "injected", False) is True
    if injected and not hasattr(native, "create_process"):
        outcome = native.try_descendant(fixed_command())
    else:
        from .probe_io_native import NativeJobBoundary

        boundary = native if hasattr(native, "create_process") else NativeJobBoundary(native)
        outcome = try_descendant(boundary, gate)
    if type(outcome) is not dict:
        raise ValueError("fixture_descendant")
    outcome.setdefault("primary_error", None)
    outcome.setdefault("cleanup_errors", [])
    known = {"creation_rejected", "job_terminated"}
    if outcome.get("classification") not in known and outcome.get("primary_error") is None:
        raise ValueError("fixture_descendant")
    if gate.veto and outcome.get("primary_error") is None:
        outcome = _mark_deadline(outcome)
    status = outcome["classification"]
    if outcome.get("primary_error") or outcome.get("cleanup_errors") or gate.veto or gate.blocked():
        status = "error"
    frame = result_frame("descendant", request, status, outcome, injected=injected)
    if gate.blocked():
        return 2, Accepted(frame + b"\n", 0, False)
    return _commit(stdout, gate, frame, request, "descendant", injected, outcome)


def _commit(stdout, gate, frame, request, mode, injected, result):
    """原 total 之后不再写失败帧，也不再 flush。内存里的帧可以留下。"""
    gate.after()
    if gate.blocked():
        frame = _unsucceed(frame, result, request, mode, injected)
        return 2, Accepted(frame + b"\n", 0, False)
    if gate.veto and _frame_success(frame):
        frame = _unsucceed(frame, result, request, mode, injected)
        gate.after()
        if gate.blocked():
            return 2, Accepted(frame + b"\n", 0, False)
    accepted = publish_bytes(stdout, gate, frame + b"\n", cleanup=not _frame_success(frame))
    if not accepted.flushed or gate.veto or gate.blocked():
        return 2, accepted
    return completion_code(loads_frame(frame)), accepted


def _ready(native, stdout, ready, gate):
    if not gate.work_open():
        raise DeadlineStop("before_ready_write")
    write_exact(stdout, ready)
    if not gate.work_open():
        raise DeadlineStop("after_ready_write")
    flush_exact(stdout)
    if not gate.work_open():
        raise DeadlineStop("after_ready_flush")
    publisher = getattr(native, "publish_ready", None)
    if publisher is not None:
        publisher(ready)
    if not gate.work_open():
        raise DeadlineStop("after_publish_ready")


def _io_now(gate):
    """execute_io 直接采样。ClockError 与 QPC OSError 都先粘滞，再交回同一次异常。"""
    source = gate.clock

    def sample():
        if gate.sticky and isinstance(gate.cause, BaseException):
            raise gate.cause
        try:
            return source()
        except (ClockError, OSError) as exc:
            gate._stick(exc)
            raise

    return sample


def _attach_pipe(exc, native, owned):
    if getattr(exc, "resources", None) is None:
        exc.resources = owned
    if getattr(exc, "native", None) is None:
        exc.native = native
    native.retained = owned


def _raise_pipe(native, owned, primary, chunk, observed):
    hold = PipeHold(native, owned, _hold_detail(primary, chunk, owned, observed))
    cause = primary if isinstance(primary, BaseException) else None
    if cause is None and owned.errors:
        cause = owned.errors[0]["error"]
    if isinstance(cause, BaseException):
        raise hold from cause
    raise hold


def _hold_detail(primary, chunk, owned, observed):
    if isinstance(primary, DeadlineStop) or observed.startswith("deadline"):
        error = "fixture_deadline"
    elif primary is not None or owned.errors:
        error = "fixture_failure"
    else:
        error = None
    return {
        "observed": observed,
        "bytes": None if type(chunk) is not bytes else len(chunk),
        "primary_error": error,
        "primary_type": None if primary is None else type(primary).__name__,
        "cleanup": [
            {"name": item["name"], "error": type(item["error"]).__name__} for item in owned.errors
        ],
    }


def _blocked_error(primary, chunk, owned):
    cleanup = [
        {"name": item["name"], "error": type(item["error"]).__name__} for item in owned.errors
    ]
    body = {
        "api": "ReadFile",
        "pipe": "anonymous",
        "writer_retained": True,
        "completed": False,
        "bytes": None if type(chunk) is not bytes else len(chunk),
        "cleanup_errors": [item["error"] for item in cleanup],
        "cleanup": cleanup,
    }
    if isinstance(primary, DeadlineStop) or primary is None:
        body["primary_error"] = "fixture_deadline"
        body["primary_type"] = "ClockError" if primary is None else type(primary).__name__
    else:
        body["primary_error"] = "fixture_failure"
        body["primary_type"] = type(primary).__name__
    return body


def _payload():
    chunks = [OUTPUT_PREFIX]
    total = len(OUTPUT_PREFIX)
    while total <= STREAM_LIMIT:
        chunks.append(OUTPUT_BODY)
        total += len(OUTPUT_BODY)
    payload = b"".join(chunks)
    if len(payload) <= STREAM_LIMIT or not payload.startswith(OUTPUT_PREFIX):
        raise RuntimeError("fixture_output")
    if OUTPUT_PREFIX == OUTPUT_BODY:
        raise RuntimeError("fixture_output")
    return payload


def _empty_io(request):
    return {
        "protocol": request["protocol"],
        "host_id": request["host_id"],
        "execution_sha256": request["execution_sha256"],
        "scripts_sha256": request["scripts_sha256"],
        "stage": request["stage"],
        "job_id": request["job_id"],
        "operation": request["operation"],
        "bytes": None,
        "sha256": None,
        "data_b64": None,
        "primary_error": "io_deadline",
        "cleanup_errors": [],
    }


def _stamp(result, code):
    item = dict(result)
    cleanup = list(item.get("cleanup_errors") or [])
    if item.get("primary_error") is None:
        item["primary_error"] = code
    elif item["primary_error"] != code and code not in cleanup:
        cleanup.append(code)
    item["cleanup_errors"] = cleanup
    return item


def _expire(result):
    item = _stamp(result, "io_deadline")
    item["data_b64"] = None
    return item


def _mark_deadline(result):
    return _stamp(result, "fixture_deadline")


def _clean(result):
    return result.get("primary_error") is None and not result.get("cleanup_errors")


def _unsucceed(frame, result, request, mode, injected):
    """只改内存里的失败事实。调用方仍不得在原 total 之后把它写出去。"""
    if not _frame_success(frame):
        return frame
    stamped = _expire(result) if mode == "file_io" else _mark_deadline(result)
    return result_frame(mode, request, "error", stamped, injected=injected)


def _frame_success(frame):
    try:
        return completion_code(loads_frame(frame)) == 0
    except ValueError, KeyError, TypeError:
        return False


def _native(native):
    if native is not None:
        return native
    from .probe_io_winapi import RealProbeFileApi

    return RealProbeFileApi()
