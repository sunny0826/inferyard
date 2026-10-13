"""Stage46 工作时钟。默认路径使用同机 QPC；注入时钟必须显式传入，不能用 0 填补缺测。"""

from __future__ import annotations

import sys


class ClockError(ValueError):
    """时钟或原期限不能使用。不把缺测改成成功。"""


def bind_request_clock(request, now_ticks=None):
    """核对 request.frequency，拒绝已到期、bool、非 int 和倒退。"""
    frequency = request["frequency"]
    task = request["task_end_ticks"]
    total = request["total_end_ticks"]
    _whole(frequency, "frequency")
    _whole(task, "task_end_ticks")
    _whole(total, "total_end_ticks")
    if total <= task:
        raise ClockError("fixture_deadline")
    source = qpc_ticks if now_ticks is None else now_ticks
    if not callable(source):
        raise ClockError("fixture_clock")
    observed = _frequency(source)
    if observed != frequency:
        raise ClockError("fixture_frequency")
    return _Clock(source, frequency, task, total)


def qpc_ticks():
    """同机 QueryPerformanceCounter。非 Windows 在加载 WinDLL 之前拒绝。"""
    kernel, ctypes = _qpc()
    value = ctypes.c_int64()
    if not kernel.QueryPerformanceCounter(ctypes.byref(value)):
        raise OSError("probe_qpc")
    return int(value.value)


def qpc_frequency():
    kernel, ctypes = _qpc()
    value = ctypes.c_int64()
    if not kernel.QueryPerformanceFrequency(ctypes.byref(value)):
        raise OSError("probe_qpc")
    return int(value.value)


class _Clock:
    def __init__(self, source, frequency, task, total):
        self.source = source
        self.frequency = frequency
        self.task = task
        self.total = total
        self.last = None
        self.injected = source is not qpc_ticks

    def __call__(self):
        value = self.source()
        _whole(value, "ticks")
        if self.last is not None and value < self.last:
            raise ClockError("fixture_clock_regression")
        self.last = value
        return value

    def task_expired(self):
        return self() >= self.task

    def total_expired(self):
        return self() >= self.total


def _frequency(source):
    if source is qpc_ticks:
        return qpc_frequency()
    marker = getattr(source, "frequency", None)
    _whole(marker, "frequency")
    return marker


def _whole(value, name):
    if type(value) is not int or isinstance(value, bool):
        raise ClockError("fixture_clock")
    if name == "frequency" and value <= 0:
        raise ClockError("fixture_clock")
    if name != "frequency" and value < 0:
        raise ClockError("fixture_clock")


def _qpc():
    if sys.platform != "win32":
        raise OSError("probe_qpc_unavailable")
    import ctypes

    if ctypes.sizeof(ctypes.c_void_p) != 8:
        raise OSError("probe_pointer_width")
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    large = ctypes.c_int64
    for name in ("QueryPerformanceCounter", "QueryPerformanceFrequency"):
        function = getattr(kernel, name)
        function.argtypes = [ctypes.POINTER(large)]
        function.restype = ctypes.c_int
    return kernel, ctypes
