"""真实 WinAPI 适配器。非 Windows 或非 64 位指针在 WinDLL 之前拒绝。"""

from __future__ import annotations

import sys

from .windows_backend_limits import (
    CREATE_BREAKAWAY_FROM_JOB,
    CREATE_SUSPENDED,
    JOB_LIMIT_FLAGS,
)

_STARTF_USESTDHANDLES = 0x00000100
_HANDLE_FLAG_INHERIT = 0x00000001
_ERROR_BROKEN_PIPE = 109
_JOB_BASIC_LIMIT = 2
_RESUME_FAILED = 0xFFFFFFFF


class Spawned:
    def __init__(self, process, thread, stdout, stderr, extra=(), unknown=()):
        self.process = process
        self.thread = thread
        self.stdout = stdout
        self.stderr = stderr
        self.extra = tuple(extra)
        self.unknown = tuple(unknown)


class SpawnIncomplete(OSError):
    """CreateProcess 已成功，随后关闭父侧多余句柄失败。原进程保留在 spawned 上。"""

    def __init__(self, spawned, cause, extra, unknown=()):
        super().__init__("spawn_incomplete")
        self.spawned = spawned
        self.cause = cause
        self.extra = tuple(extra)
        self.unknown = tuple(unknown)


class CloseAttempt:
    """一次原句柄关闭尝试。ok=False 表示结果未知，不再关闭该句柄。"""

    def __init__(self, handle, ok, error=None):
        self.handle = handle
        self.ok = ok
        self.error = error


class PipeResources:
    """进程尚未创建时已创建的原管道句柄。pending 是尚未尝试关闭的句柄。"""

    def __init__(self, handles):
        self.handles = tuple(handles)
        self.pending = [handle for handle in self.handles if handle]


class PipeCleanupUnknown(OSError):
    """进程尚未创建，但已创建的原管道关闭结果不确定。"""

    def __init__(self, cause, resources, closes):
        super().__init__("pipe_cleanup_unknown")
        self.cause = cause
        self.resources = resources
        self.closes = tuple(closes)
        self.close_error = next(
            (item.error for item in self.closes if item.error is not None), None
        )


class RealWindowsApi:
    def __init__(self):
        self._win32()
        self._kernel = None

    def spawn_suspended(self, argv, cwd, creation_flags):
        self._win32()
        if creation_flags != CREATE_SUSPENDED or creation_flags & CREATE_BREAKAWAY_FROM_JOB:
            raise OSError("creation_flags_rejected")
        kernel, ctypes, wintypes = self._dll()
        ends = self._pipes(kernel, ctypes, wintypes)
        stdin_read, stdin_write, stdout_read, stdout_write, stderr_read, stderr_write = ends
        self._clear_inherit(kernel, ctypes, (stdin_write, stdout_read, stderr_read), ends)
        startup = self._startup(ctypes, wintypes, stdin_read, stdout_write, stderr_write)
        info = self._process_info(ctypes, wintypes)
        command = ctypes.create_unicode_buffer(_command_line(argv))
        ok = kernel.CreateProcessW(
            argv[0],
            command,
            None,
            None,
            True,
            creation_flags,
            None,
            cwd,
            ctypes.byref(startup),
            ctypes.byref(info),
        )
        if not ok:
            error = _win_error(ctypes)
            self._abandon_pipes(kernel, ctypes, ends, error)
        spawned = Spawned(info.hProcess, info.hThread, stdout_read, stderr_read)
        parent_ends = [stdin_read, stdin_write, stdout_write, stderr_write]
        while parent_ends:
            handle = parent_ends.pop(0)
            try:
                self._must_close(kernel, ctypes, handle)
            except OSError as exc:
                failed = SpawnIncomplete(spawned, exc, parent_ends, (handle,))
                raise failed from exc
        return spawned

    def create_job(self, flags, active_limit):
        self._win32()
        if flags != JOB_LIMIT_FLAGS or active_limit != 1:
            raise OSError("job_limits_rejected")
        kernel, ctypes, wintypes = self._dll()
        job = kernel.CreateJobObjectW(None, None)
        if not job:
            raise _win_error(ctypes)
        info = _basic_limits(ctypes)
        info.LimitFlags = flags
        info.ActiveProcessLimit = active_limit
        ok = kernel.SetInformationJobObject(
            job, _JOB_BASIC_LIMIT, ctypes.byref(info), ctypes.sizeof(info)
        )
        if not ok:
            error = _win_error(ctypes)
            try:
                self._must_close(kernel, ctypes, job)
            except OSError as close_error:
                error.unknown_handles = (job,)
                raise error from close_error
            raise error
        return job

    def assign_job(self, job, process):
        self._win32()
        kernel, ctypes, _wintypes = self._dll()
        if not kernel.AssignProcessToJobObject(job, process):
            raise _win_error(ctypes)

    def resume(self, thread):
        self._win32()
        kernel, ctypes, _wintypes = self._dll()
        previous = int(kernel.ResumeThread(thread))
        if previous == _RESUME_FAILED:
            raise _win_error(ctypes)
        if previous != 1:
            raise OSError("resume_count")

    def peek(self, pipe):
        self._win32()
        kernel, ctypes, _wintypes = self._dll()
        available = ctypes.c_uint32()
        ok = kernel.PeekNamedPipe(pipe, None, 0, None, ctypes.byref(available), None)
        if not ok:
            error = _last_error(ctypes)
            if error == _ERROR_BROKEN_PIPE:
                return 0, True
            raise _win_error(ctypes, error)
        return int(available.value), False

    def read(self, pipe, size):
        self._win32()
        if type(size) is not int or size <= 0:
            raise OSError("read_size")
        kernel, ctypes, _wintypes = self._dll()
        buffer = ctypes.create_string_buffer(size)
        got = ctypes.c_uint32()
        ok = kernel.ReadFile(pipe, buffer, size, ctypes.byref(got), None)
        if not ok:
            error = _last_error(ctypes)
            if error == _ERROR_BROKEN_PIPE:
                return b""
            raise _win_error(ctypes, error)
        return buffer.raw[: int(got.value)]

    def wait_zero(self, process):
        self._win32()
        kernel, _ctypes, _wintypes = self._dll()
        return int(kernel.WaitForSingleObject(process, 0))

    def get_exit_code(self, process):
        self._win32()
        kernel, ctypes, _wintypes = self._dll()
        code = ctypes.c_uint32()
        if not kernel.GetExitCodeProcess(process, ctypes.byref(code)):
            raise _win_error(ctypes)
        return int(code.value)

    def terminate(self, process):
        self._end(process, 1)

    def kill(self, process):
        self._end(process, 9)

    def close(self, handle):
        self._win32()
        kernel, ctypes, _wintypes = self._dll()
        self._must_close(kernel, ctypes, handle)

    def _end(self, process, code):
        self._win32()
        kernel, ctypes, _wintypes = self._dll()
        if not kernel.TerminateProcess(process, code):
            raise _win_error(ctypes)

    def _win32(self):
        if sys.platform != "win32":
            raise OSError("winapi_unavailable")

    def _dll(self):
        self._win32()
        import ctypes

        if ctypes.sizeof(ctypes.c_void_p) != 8:
            raise OSError("winapi_pointer_width")
        from ctypes import wintypes

        if self._kernel is None:
            self._kernel = ctypes.WinDLL("kernel32", use_last_error=True)
            _bind(self._kernel, ctypes)
        return self._kernel, ctypes, wintypes

    def _pipes(self, kernel, ctypes, wintypes):
        security = _security(ctypes, wintypes)
        ends = []
        for _pair in range(3):
            read = wintypes.HANDLE()
            write = wintypes.HANDLE()
            ok = kernel.CreatePipe(
                ctypes.byref(read), ctypes.byref(write), ctypes.byref(security), 0
            )
            if not ok:
                error = _win_error(ctypes)
                self._abandon_pipes(kernel, ctypes, ends, error)
            ends.extend((read.value, write.value))
        return tuple(ends)

    def _clear_inherit(self, kernel, ctypes, handles, ends):
        for handle in handles:
            if not kernel.SetHandleInformation(handle, _HANDLE_FLAG_INHERIT, 0):
                self._abandon_pipes(kernel, ctypes, ends, _win_error(ctypes))

    def _startup(self, ctypes, wintypes, stdin_read, stdout_write, stderr_write):
        info = _startup_info(ctypes, wintypes)
        info.cb = ctypes.sizeof(info)
        info.dwFlags = _STARTF_USESTDHANDLES
        info.hStdInput = stdin_read
        info.hStdOutput = stdout_write
        info.hStdError = stderr_write
        return info

    def _process_info(self, ctypes, wintypes):
        return _process_information(ctypes, wintypes)

    def _must_close(self, kernel, ctypes, handle):
        if not kernel.CloseHandle(handle):
            raise _win_error(ctypes)

    def _abandon_pipes(self, kernel, ctypes, handles, cause):
        resources = PipeResources(handles)
        closes = self._close_pending(kernel, resources)
        if any(not item.ok for item in closes) or resources.pending:
            cleanup = PipeCleanupUnknown(cause, resources, closes)
            if cleanup.close_error is not None:
                cause.__cause__ = cleanup.close_error
            raise cleanup from cause
        raise cause

    def _close_pending(self, kernel, resources):
        closes = []
        while resources.pending:
            handle = resources.pending[0]
            try:
                ok = bool(kernel.CloseHandle(handle))
            except Exception as exc:
                closes.append(CloseAttempt(handle, False, exc))
                break
            closes.append(CloseAttempt(handle, ok))
            if not ok:
                break
            resources.pending.pop(0)
        return closes


def _last_error(ctypes):
    get = getattr(ctypes, "get_last_error", None)
    if get is None:
        return 0
    return get()


def _win_error(ctypes, code=None):
    if code is None:
        code = _last_error(ctypes)
    factory = getattr(ctypes, "WinError", None)
    if factory is None:
        return OSError(code)
    return factory(code)


def _bind(kernel, ctypes):
    """给本次实际调用的 WinAPI 声明参数和返回宽度。HANDLE 使用指针宽度，不用默认 c_int。"""
    handle = ctypes.c_void_p
    dword = ctypes.c_uint32
    boolean = ctypes.c_int
    wide = ctypes.c_wchar_p
    void = ctypes.c_void_p
    pdword = ctypes.POINTER(dword)
    phandle = ctypes.POINTER(handle)
    specs = {
        "CreateProcessW": (
            [wide, wide, void, void, boolean, dword, void, wide, void, void],
            boolean,
        ),
        "CreatePipe": ([phandle, phandle, void, dword], boolean),
        "SetHandleInformation": ([handle, dword, dword], boolean),
        "CloseHandle": ([handle], boolean),
        "CreateJobObjectW": ([void, wide], handle),
        "SetInformationJobObject": ([handle, dword, void, dword], boolean),
        "AssignProcessToJobObject": ([handle, handle], boolean),
        "ResumeThread": ([handle], dword),
        "PeekNamedPipe": ([handle, void, dword, pdword, pdword, pdword], boolean),
        "ReadFile": ([handle, void, dword, pdword, void], boolean),
        "WaitForSingleObject": ([handle, dword], dword),
        "GetExitCodeProcess": ([handle, pdword], boolean),
        "TerminateProcess": ([handle, dword], boolean),
    }
    for name, (args, result) in specs.items():
        function = getattr(kernel, name, None)
        if function is None:
            continue
        function.argtypes = args
        function.restype = result


def basic_limits_nbytes():
    """64 位布局的结构大小。不调用 WinDLL。"""
    import ctypes

    return ctypes.sizeof(_basic_limits(ctypes))


def _command_line(argv):
    return " ".join(_quote(arg) for arg in argv)


def _quote(arg):
    if arg == "":
        return '""'
    if not any(char in arg for char in ' \t"'):
        return arg
    quoted = ['"']
    slashes = 0
    for char in arg:
        if char == "\\":
            slashes += 1
            continue
        if char == '"':
            quoted.append("\\" * (slashes * 2 + 1))
            quoted.append('"')
        else:
            if slashes:
                quoted.append("\\" * slashes)
            quoted.append(char)
        slashes = 0
    if slashes:
        quoted.append("\\" * (slashes * 2))
    quoted.append('"')
    return "".join(quoted)


def _security(ctypes, wintypes):
    class SecurityAttributes(ctypes.Structure):
        _fields_ = [
            ("nLength", wintypes.DWORD),
            ("lpSecurityDescriptor", ctypes.c_void_p),
            ("bInheritHandle", wintypes.BOOL),
        ]

    value = SecurityAttributes()
    value.nLength = ctypes.sizeof(value)
    value.bInheritHandle = True
    return value


def _startup_info(ctypes, wintypes):
    class StartupInfo(ctypes.Structure):
        _fields_ = [
            ("cb", wintypes.DWORD),
            ("lpReserved", wintypes.LPWSTR),
            ("lpDesktop", wintypes.LPWSTR),
            ("lpTitle", wintypes.LPWSTR),
            ("dwX", wintypes.DWORD),
            ("dwY", wintypes.DWORD),
            ("dwXSize", wintypes.DWORD),
            ("dwYSize", wintypes.DWORD),
            ("dwXCountChars", wintypes.DWORD),
            ("dwYCountChars", wintypes.DWORD),
            ("dwFillAttribute", wintypes.DWORD),
            ("dwFlags", wintypes.DWORD),
            ("wShowWindow", wintypes.WORD),
            ("cbReserved2", wintypes.WORD),
            ("lpReserved2", ctypes.c_void_p),
            ("hStdInput", wintypes.HANDLE),
            ("hStdOutput", wintypes.HANDLE),
            ("hStdError", wintypes.HANDLE),
        ]

    return StartupInfo()


def _process_information(ctypes, wintypes):
    class ProcessInformation(ctypes.Structure):
        _fields_ = [
            ("hProcess", wintypes.HANDLE),
            ("hThread", wintypes.HANDLE),
            ("dwProcessId", wintypes.DWORD),
            ("dwThreadId", wintypes.DWORD),
        ]

    return ProcessInformation()


def _basic_limits(ctypes):
    # Windows DWORD 固定 32 位。macOS 上 wintypes.DWORD 随 C long 变成 8 字节，不能拿来套这个结构。
    dword = ctypes.c_uint32

    class BasicLimits(ctypes.Structure):
        # 64 位 JOBOBJECT_BASIC_LIMIT_INFORMATION。本波不在 Windows 上执行。
        _fields_ = [
            ("PerProcessUserTimeLimit", ctypes.c_int64),
            ("PerJobUserTimeLimit", ctypes.c_int64),
            ("LimitFlags", dword),
            ("_pad_flags", dword),
            ("MinimumWorkingSetSize", ctypes.c_size_t),
            ("MaximumWorkingSetSize", ctypes.c_size_t),
            ("ActiveProcessLimit", dword),
            ("_pad_limit", dword),
            ("Affinity", ctypes.c_size_t),
            ("PriorityClass", dword),
            ("SchedulingClass", dword),
        ]

    return BasicLimits()
