"""Stage46 真实后代调用边界。只在 Windows worker 内由 RealProbeFileApi 使用。"""

from __future__ import annotations

from .probe_io_structs import (
    basic_accounting,
    command_line,
    extended_limits,
    process_information,
    startup_info,
)

_JOB_BASIC_ACCOUNTING = 1
_JOB_EXTENDED_LIMIT = 9


class InheritedJob:
    """当前进程所在 Job 的查询标记。不是 HANDLE，不能传给 AssignProcessToJobObject。"""

    def __bool__(self):
        return True


class NativeJobBoundary:
    """CreateProcess 继承当前 Job。查询可以用 NULL；赋值不可以。不另造 Job，也不从 PID 重建。"""

    def __init__(self, api, gate=None):
        self.api = api
        self.retained = None
        self.gate = gate
        self.created_handles = None

    def current_job(self):
        kernel, ctypes = self.api._dll()
        self._before_work()
        current = kernel.GetCurrentProcess()
        self._after_work()
        self._before_work()
        assigned = ctypes.c_int()
        ok = kernel.IsProcessInJob(current, None, ctypes.byref(assigned))
        self._after_work()
        if not ok or not int(assigned.value):
            raise OSError("job_not_inherited")
        return InheritedJob()

    def job_limits(self, job):
        if type(job) is not InheritedJob:
            raise OSError("job_handle_unavailable")
        self._before_work()
        kernel, ctypes = self.api._dll()
        info = extended_limits(ctypes)
        ok = kernel.QueryInformationJobObject(
            None,
            _JOB_EXTENDED_LIMIT,
            ctypes.byref(info),
            ctypes.sizeof(info),
            None,
        )
        self._after_work()
        if not ok:
            raise self._error(ctypes)
        basic = info.BasicLimitInformation
        return {
            "flags": int(basic.LimitFlags),
            "active_process_limit": int(basic.ActiveProcessLimit),
            "query": "QueryInformationJobObject",
            "job_handle": None,
        }

    def create_process(self, argv, flags, inherit):
        self._before_work()
        kernel, ctypes = self.api._dll()
        info = process_information(ctypes)
        command = ctypes.create_unicode_buffer(command_line(argv))
        ok = kernel.CreateProcessW(
            argv[0],
            command,
            None,
            None,
            bool(inherit),
            flags,
            None,
            None,
            ctypes.byref(startup_info(ctypes)),
            ctypes.byref(info),
        )
        if not ok:
            error = self._error(ctypes)
            error.api = "CreateProcessW"
            self._after_work()
            raise error
        process, thread = info.hProcess, info.hThread
        self.created_handles = (process, thread)
        self._after_work()
        return process, thread

    def _before_work(self):
        if self.gate is None:
            return
        if not self.gate.work_open():
            from .probe_fixture_custody import DeadlineStop

            raise DeadlineStop("job_work")

    def _after_work(self):
        if self.gate is not None:
            self.gate.after()

    def assign_job(self, job, process):
        """没有 CreateJobObject/OpenJobObject 句柄时不能赋值。NULL 与进程伪句柄都拒绝。"""
        _ = (job, process)
        raise OSError("assign_job_handle_unavailable")

    def resume(self, thread):
        kernel, _ctypes = self.api._dll()
        previous = int(kernel.ResumeThread(thread))
        if previous == 0xFFFFFFFF:
            raise self._error(_ctypes)

    def wait(self, handle, milliseconds):
        kernel, _ctypes = self.api._dll()
        return int(kernel.WaitForSingleObject(handle, milliseconds))

    def exit_code(self, process):
        kernel, ctypes = self.api._dll()
        code = ctypes.c_uint32()
        if not kernel.GetExitCodeProcess(process, ctypes.byref(code)):
            raise self._error(ctypes)
        return int(code.value)

    def job_basic_accounting(self, job):
        """NULL 查询当前 Job 的累计计数。该计数不能证明当前这次子进程被限制终止。"""
        if type(job) is not InheritedJob:
            raise OSError("job_handle_unavailable")
        kernel, ctypes = self.api._dll()
        info = basic_accounting(ctypes)
        ok = kernel.QueryInformationJobObject(
            None,
            _JOB_BASIC_ACCOUNTING,
            ctypes.byref(info),
            ctypes.sizeof(info),
            None,
        )
        if not ok:
            raise self._error(ctypes)
        return {
            "total_terminated": int(info.TotalTerminatedProcesses),
            "query": "QueryInformationJobObject",
            "causal": False,
        }

    def terminate(self, process):
        kernel, ctypes = self.api._dll()
        if not kernel.TerminateProcess(process, 1):
            raise self._error(ctypes)

    def close(self, handle):
        self.api.close(handle)

    def _error(self, ctypes):
        from .probe_io_winapi import _win_error

        return _win_error(ctypes)
