"""Stage46 Job 句柄边界。NULL 可以查询当前 Job，不能传给 AssignProcessToJobObject。"""

import ctypes

from scripts.ninfer_source_host.probe_io_native import InheritedJob, NativeJobBoundary
from scripts.ninfer_source_host.probe_io_winapi import RealProbeFileApi
from tests.unit.test_ninfer_source_probe_io import HIGH


class _Kernel:
    def __init__(self):
        self.calls = []

    def GetCurrentProcess(self):
        return 11

    def IsProcessInJob(self, process, job, assigned):
        self.calls.append(("job", process, job))
        assigned._obj.value = 1
        return 1

    def AssignProcessToJobObject(self, job, process):
        self.calls.append(("assign", job, process))
        return 1

    def QueryInformationJobObject(self, job, kind, info, size, returned):
        self.calls.append(("query", kind, job))
        info._obj.BasicLimitInformation.LimitFlags = 0x8
        info._obj.BasicLimitInformation.ActiveProcessLimit = 1
        return 1


def test_current_job_is_not_a_process_pseudo_handle_and_assign_rejects_null():
    kernel = _Kernel()
    raw = RealProbeFileApi.__new__(RealProbeFileApi)
    raw._dll = lambda: (kernel, ctypes)
    boundary = NativeJobBoundary(raw)
    job = boundary.current_job()
    assert type(job) is InheritedJob
    assert job is not kernel.GetCurrentProcess()
    assert ("job", 11, None) in kernel.calls
    limits = boundary.job_limits(job)
    assert limits["job_handle"] is None
    assert ("query", 9, None) in kernel.calls
    try:
        boundary.assign_job(None, HIGH)
    except OSError as exc:
        assert str(exc) == "assign_job_handle_unavailable"
    else:
        raise AssertionError("NULL job assignment was accepted")
    assert not any(call[0] == "assign" for call in kernel.calls)
    try:
        boundary.assign_job(job, HIGH)
    except OSError as exc:
        assert str(exc) == "assign_job_handle_unavailable"
    else:
        raise AssertionError("query marker was accepted as a job handle")
    assert not any(call[0] == "assign" for call in kernel.calls)
