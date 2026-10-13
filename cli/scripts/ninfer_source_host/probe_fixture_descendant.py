"""Stage46 固定后代尝试。子进程继承当前 Job；没有 Job 句柄时不调用 Assign。"""

from __future__ import annotations

import sys

from .probe_io_limits import (
    ACTIVE_PROCESS_LIMIT,
    CREATE_BREAKAWAY_FROM_JOB,
    CREATE_SUSPENDED,
    DESCENDANT_ARGV,
    JOB_OBJECT_LIMIT_ACTIVE_PROCESS,
    JOB_OBJECT_LIMIT_BREAKAWAY_OK,
    JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK,
)

_WAIT_OBJECT_0 = 0
_WAIT_TIMEOUT = 258
_STILL_ACTIVE = 259


class DescendantHold(Exception):
    """已创建但未确认退出。原 process/thread 仍由 resources 持有，调用方不得再关闭。"""

    def __init__(self, resources, detail, native=None):
        super().__init__("fixture_descendant_unknown")
        self.resources = resources
        self.detail = detail
        self.native = native


def fixed_command():
    """同一已批准解释器，固定无文件、无模型命令。不用 PATH 上的 python。"""
    if type(sys.executable) is not str or not sys.executable or "\0" in sys.executable:
        raise RuntimeError("fixture_interpreter")
    return (sys.executable, *DESCENDANT_ARGV[1:])


def try_descendant(native, gate=None):
    """真实尝试的可注入边界。native 必须提供 create_process，不能是文件适配器本身。"""
    if native is None or not hasattr(native, "create_process"):
        raise AttributeError("try_descendant")
    command = fixed_command()
    _reject_breakaway()
    held = _Resources()
    if gate is not None and hasattr(native, "gate"):
        native.gate = gate
    try:
        return _attempt(native, command, held, gate)
    except DescendantHold as hold:
        _bind(hold, native, held)
        cause = hold.__cause__
        if isinstance(cause, BaseException):
            _bind(cause, native, held)
        raise
    except BaseException as exc:
        _keep_created(native, held)
        if held.process is not None:
            _bind(exc, native, held)
        raise


def _attempt(native, command, held, gate):
    stopped = _stop_work(gate, "deadline_before_create")
    if stopped is not None:
        return stopped
    job = _call(native, "current_job")
    stopped = _stop_work(gate, "deadline_after_current_job")
    if stopped is not None:
        return stopped
    _require_inherited_limit(native, job)
    stopped = _stop_work(gate, "deadline_after_job_limits")
    if stopped is not None:
        return stopped
    try:
        created = _call(native, "create_process", command, CREATE_SUSPENDED, False)
    except OSError as exc:
        if _keep_created(native, held):
            _bind(exc, native, held)
            raise
        if gate is not None and gate.sticky:
            raise
        if gate is not None:
            gate.after()
        return _rejected(exc, gate)
    process, thread = _created(created)
    held.keep(process, thread)
    _take_created(native)
    if gate is not None and not gate.work_open():
        raise DescendantHold(
            held,
            {
                "observed": "deadline_after_create",
                "api": "CreateProcessW",
                "ticks": gate.sampled,
            },
        )
    wait = _call(native, "wait", process, 0)
    if gate is not None and not gate.work_open():
        raise DescendantHold(
            held,
            {
                "observed": "deadline_after_wait",
                "api": "WaitForSingleObject",
                "wait": wait,
            },
        )
    code = _call(native, "exit_code", process)
    if gate is not None and not gate.work_open():
        raise DescendantHold(
            held,
            {
                "observed": "deadline_after_exit",
                "api": "GetExitCodeProcess",
                "wait": wait,
                "exit_code": code,
            },
        )
    if (
        wait == _WAIT_TIMEOUT
        or code == _STILL_ACTIVE
        or type(code) is not int
        or isinstance(code, bool)
    ):
        raise DescendantHold(
            held,
            {
                "observed": "exit_unconfirmed",
                "api": "WaitForSingleObject",
                "wait": wait,
                "exit_code": code,
            },
        )
    if wait != _WAIT_OBJECT_0:
        raise DescendantHold(
            held,
            {
                "observed": "exit_unconfirmed",
                "api": "WaitForSingleObject",
                "wait": wait,
                "exit_code": code,
            },
        )
    if gate is not None and not gate.cleanup_open():
        raise DescendantHold(
            held,
            {
                "observed": "deadline_before_close",
                "api": "CreateProcessW",
                "wait": wait,
                "exit_code": code,
                "ticks": gate.sampled,
            },
        )
    held.close_once(native, gate)
    if not held.fully_closed():
        detail = {
            "observed": "close_unknown" if held.errors else "close_incomplete",
            "api": "CloseHandle",
            "wait": wait,
            "exit_code": code,
            "handles": held.report(),
        }
        hold = DescendantHold(held, detail)
        if held.errors and isinstance(held.errors[0]["error"], BaseException):
            raise hold from held.errors[0]["error"]
        raise hold
    outcome = _observed(code, wait, held)
    if gate is not None and gate.veto and outcome.get("primary_error") is None:
        outcome["primary_error"] = "fixture_deadline"
    return outcome


def _take_created(native):
    """本次 pending 只转移一次。清空槽位，不碰已经交给 ledger 的对象。"""
    pending = getattr(native, "created_handles", None)
    if hasattr(native, "created_handles"):
        native.created_handles = None
    return pending


def _keep_created(native, held):
    """CreateProcess 已经返回句柄时，采样失败不能再记成创建被拒绝。"""
    if held.process is not None:
        _take_created(native)
        return True
    pending = getattr(native, "created_handles", None)
    if pending is None:
        return False
    held.keep(pending[0], pending[1])
    _take_created(native)
    return True


def _bind(exc, native, held):
    """同一异常带走原 resources 和原适配器。不关闭，不换对象。"""
    _anchor(native, held)
    if getattr(exc, "resources", None) is None:
        exc.resources = held
    if getattr(exc, "native", None) is None:
        api = getattr(native, "api", None)
        exc.native = api if api is not None else native


def _stop_work(gate, observed):
    if gate is None or gate.work_open():
        return None
    return _deadline(observed, gate)


def _rejected(error, gate=None):
    detail = {
        "api": getattr(error, "api", None) or "CreateProcessW",
        "winerror": _winerror(error),
        "observed": "create_process_refused",
    }
    if gate is not None:
        detail["ticks"] = gate.sampled
    return {
        "classification": "creation_rejected",
        "detail": detail,
        "primary_error": None,
        "cleanup_errors": [],
        "handles": _Resources().report(),
    }


def _observed(code, wait, held):
    detail = {
        "api": "CreateProcessW",
        "wait": wait,
        "exit_code": code,
        "assigned": False,
        "resumed": False,
        "terminated_by_fixture": False,
    }
    report = held.report()
    if held.errors:
        detail["observed"] = "close_unknown"
        return {
            "classification": "close_unknown",
            "detail": detail,
            "primary_error": "fixture_cleanup",
            "cleanup_errors": [item["error"] for item in held.errors],
            "handles": report,
        }
    if code == 0:
        detail["observed"] = "normal_exit_not_job_termination"
        return {
            "classification": "normal_exit",
            "detail": detail,
            "primary_error": "fixture_descendant",
            "cleanup_errors": [],
            "handles": report,
        }
    detail["observed"] = "inherited_limit_terminated"
    return {
        "classification": "job_terminated",
        "detail": detail,
        "primary_error": None,
        "cleanup_errors": [],
        "handles": report,
    }


def _deadline(observed, gate=None):
    detail = {"observed": observed, "api": "CreateProcessW"}
    if gate is not None:
        detail["ticks"] = gate.sampled
    return {
        "classification": "deadline",
        "detail": detail,
        "primary_error": "fixture_deadline",
        "cleanup_errors": [],
        "handles": _Resources().report(),
    }


def _require_inherited_limit(native, job):
    if not job:
        raise RuntimeError("fixture_job_missing")
    limits = _call(native, "job_limits", job)
    if type(limits) is not dict:
        raise RuntimeError("fixture_job")
    flags = limits.get("flags")
    active = limits.get("active_process_limit")
    if type(flags) is not int or type(active) is not int:
        raise RuntimeError("fixture_job")
    if flags & (JOB_OBJECT_LIMIT_BREAKAWAY_OK | JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK):
        raise RuntimeError("fixture_job_breakaway")
    if not flags & JOB_OBJECT_LIMIT_ACTIVE_PROCESS or active != ACTIVE_PROCESS_LIMIT:
        raise RuntimeError("fixture_job_limit")


def _created(value):
    if type(value) is not tuple or len(value) != 2 or not value[0] or not value[1]:
        raise RuntimeError("fixture_descendant_unknown")
    return value


def _winerror(error):
    code = getattr(error, "winerror", None)
    if type(code) is int and not isinstance(code, bool):
        return code
    if type(error.args) is tuple and error.args:
        first = error.args[0]
        if type(first) is int and not isinstance(first, bool):
            return first
    return None


def _reject_breakaway():
    if CREATE_SUSPENDED & CREATE_BREAKAWAY_FROM_JOB:
        raise RuntimeError("fixture_job")


def _anchor(native, resources):
    native.retained = resources
    api = getattr(native, "api", None)
    if api is not None:
        api.retained = resources


def _call(native, name, *args):
    method = getattr(native, name, None)
    if method is None:
        raise AttributeError(name)
    return method(*args)


class _Resources:
    def __init__(self):
        self.process = None
        self.thread = None
        self.tried = {}
        self.closed = {}
        self.errors = []

    def keep(self, process, thread):
        if not process or not thread:
            raise RuntimeError("fixture_descendant_unknown")
        self.process = process
        self.thread = thread

    def close_once(self, native, gate=None):
        for name in ("thread", "process"):
            handle = getattr(self, name)
            if handle is None or self.tried.get(name):
                continue
            if gate is not None and gate.blocked():
                break
            self.tried[name] = True
            try:
                native.close(handle)
            except BaseException as exc:
                self.closed[name] = False
                self.errors.append({"name": name, "error": exc})
                if gate is not None:
                    gate.after()
                if isinstance(exc, (KeyboardInterrupt, SystemExit)):
                    raise
            else:
                self.closed[name] = True
            if gate is not None:
                gate.after()

    def fully_closed(self):
        return bool(self.closed.get("thread")) and bool(self.closed.get("process"))

    def report(self):
        names = [name for name in ("thread", "process") if getattr(self, name) is not None]
        return {
            "attempted": {name: self.tried.get(name, False) for name in names},
            "closed": {name: self.closed.get(name, False) for name in names},
            "unknown": [
                name for name in names if self.tried.get(name) and not self.closed.get(name)
            ],
            "errors": [
                item["error"] if type(item["error"]) is str else type(item["error"]).__name__
                for item in self.errors
            ],
        }
