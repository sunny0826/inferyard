"""Stage46 文件 WinAPI。非 Windows 在 WinDLL 之前拒绝；HANDLE 使用指针宽度。"""

from __future__ import annotations

import sys

from .probe_fixture_descendant import fixed_command, try_descendant
from .probe_io_limits import (
    CREATE_BREAKAWAY_FROM_JOB,
    CREATE_NEW,
    CREATE_SUSPENDED,
    ERROR_ALREADY_EXISTS,
    ERROR_FILE_EXISTS,
    ERROR_FILE_NOT_FOUND,
    ERROR_PATH_NOT_FOUND,
    FILE_ATTRIBUTE_NORMAL,
    FILE_ATTRIBUTE_REPARSE_POINT,
    FILE_FLAG_BACKUP_SEMANTICS,
    FILE_FLAG_OPEN_REPARSE_POINT,
    FILE_READ_ATTRIBUTES,
    FILE_READ_DATA,
    FILE_SHARE_READ,
    FILE_SHARE_WRITE,
    FILE_WRITE_DATA,
    INVALID_FILE_ATTRIBUTES,
    OPEN_EXISTING,
)
from .probe_io_native import NativeJobBoundary
from .probe_io_structs import basic_limits as _basic_limits
from .probe_io_structs import command_line as _command_line
from .probe_io_structs import process_information as _process_information
from .probe_io_structs import startup_info as _startup_info

_INVALID = -1
_JOB_BASIC_LIMIT = 2


class RealProbeFileApi:
    """真实文件句柄适配。构造和每个方法都先拒绝非 Windows，不在 Mac 上加载 WinDLL。"""

    def __init__(self):
        self._win32()
        self._kernel = None

    def attributes(self, path):
        self._win32()
        kernel, ctypes = self._dll()
        value = int(kernel.GetFileAttributesW(path))
        if value == INVALID_FILE_ATTRIBUTES:
            code = _last_error(ctypes)
            if code in {ERROR_FILE_NOT_FOUND, ERROR_PATH_NOT_FOUND}:
                raise _win_error(ctypes, code)
            raise _win_error(ctypes, code)
        return value

    def try_descendant(self, argv):
        """真实可用边界。本方法不在 Mac 调用，也不在本波启动进程。"""
        self._win32()
        expected = fixed_command()
        if tuple(argv) != expected:
            raise OSError("descendant_command")
        return try_descendant(NativeJobBoundary(self))

    def open_existing(self, path, access, flags):
        return self._open(path, access, OPEN_EXISTING, flags)

    def create_new(self, path):
        return self._open(path, FILE_WRITE_DATA, CREATE_NEW, FILE_ATTRIBUTE_NORMAL)

    def read(self, handle, size):
        self._win32()
        if type(size) is not int or isinstance(size, bool) or size < 0:
            raise OSError("read_size")
        kernel, ctypes = self._dll()
        buffer = ctypes.create_string_buffer(size)
        got = ctypes.c_uint32()
        ok = kernel.ReadFile(handle, buffer, size, ctypes.byref(got), None)
        if not ok:
            raise _win_error(ctypes)
        return buffer.raw[: int(got.value)]

    def write(self, handle, data):
        self._win32()
        if type(data) is not bytes:
            raise OSError("write_type")
        kernel, ctypes = self._dll()
        written = ctypes.c_uint32()
        ok = kernel.WriteFile(handle, data, len(data), ctypes.byref(written), None)
        if not ok:
            raise _win_error(ctypes)
        return int(written.value)

    def flush_buffers(self, handle):
        self._win32()
        kernel, ctypes = self._dll()
        if not kernel.FlushFileBuffers(handle):
            raise _win_error(ctypes)

    def close(self, handle):
        self._win32()
        kernel, ctypes = self._dll()
        if not kernel.CloseHandle(handle):
            raise _win_error(ctypes)

    def create_pipe(self):
        self._win32()
        kernel, ctypes = self._dll()
        read = ctypes.c_void_p()
        write = ctypes.c_void_p()
        if not kernel.CreatePipe(ctypes.byref(read), ctypes.byref(write), None, 0):
            raise _win_error(ctypes)
        return read.value, write.value

    def spawn_suspended(self, argv, cwd):
        self._win32()
        kernel, ctypes = self._dll()
        info = _process_information(ctypes)
        command = ctypes.create_unicode_buffer(_command_line(argv))
        ok = kernel.CreateProcessW(
            argv[0],
            command,
            None,
            None,
            False,
            CREATE_SUSPENDED,
            None,
            cwd,
            ctypes.byref(_startup_info(ctypes)),
            ctypes.byref(info),
        )
        if not ok:
            raise _win_error(ctypes)
        return info.hProcess, info.hThread

    def create_limited_job(self):
        self._win32()
        kernel, ctypes = self._dll()
        job = kernel.CreateJobObjectW(None, None)
        if not job:
            raise _win_error(ctypes)
        info = _basic_limits(ctypes)
        info.LimitFlags = 0x8
        info.ActiveProcessLimit = 1
        ok = kernel.SetInformationJobObject(
            job, _JOB_BASIC_LIMIT, ctypes.byref(info), ctypes.sizeof(info)
        )
        if not ok:
            error = _win_error(ctypes)
            try:
                if not kernel.CloseHandle(job):
                    error.unknown_handles = (job,)
            except Exception as close_error:
                error.unknown_handles = (job,)
                raise error from close_error
            raise error
        return job

    def assign_job(self, job, process):
        self._win32()
        kernel, ctypes = self._dll()
        if not kernel.AssignProcessToJobObject(job, process):
            raise _win_error(ctypes)

    def resume(self, thread):
        self._win32()
        kernel, ctypes = self._dll()
        previous = int(kernel.ResumeThread(thread))
        if previous == 0xFFFFFFFF:
            raise _win_error(ctypes)

    def wait(self, handle, milliseconds):
        self._win32()
        kernel, _ctypes = self._dll()
        return int(kernel.WaitForSingleObject(handle, milliseconds))

    def exit_code(self, process):
        self._win32()
        kernel, ctypes = self._dll()
        code = ctypes.c_uint32()
        if not kernel.GetExitCodeProcess(process, ctypes.byref(code)):
            raise _win_error(ctypes)
        return int(code.value)

    def _open(self, path, access, disposition, flags):
        self._win32()
        kernel, ctypes = self._dll()
        handle = kernel.CreateFileW(
            path,
            access,
            FILE_SHARE_READ | FILE_SHARE_WRITE,
            None,
            disposition,
            flags,
            None,
        )
        if _missing(handle):
            error = _win_error(ctypes)
            if _last_error(ctypes) in {ERROR_FILE_EXISTS, ERROR_ALREADY_EXISTS}:
                raise FileExistsError(ERROR_FILE_EXISTS, "create_new") from error
            raise error
        return handle

    def _win32(self):
        if sys.platform != "win32":
            raise OSError("probe_winapi_unavailable")

    def _dll(self):
        self._win32()
        import ctypes

        if ctypes.sizeof(ctypes.c_void_p) != 8:
            raise OSError("probe_pointer_width")
        if self._kernel is None:
            self._kernel = ctypes.WinDLL("kernel32", use_last_error=True)
            _bind(self._kernel, ctypes)
        return self._kernel, ctypes


def reparse_flag():
    return FILE_ATTRIBUTE_REPARSE_POINT


def attribute_access():
    return FILE_READ_ATTRIBUTES | FILE_FLAG_OPEN_REPARSE_POINT | FILE_FLAG_BACKUP_SEMANTICS


def read_access():
    return FILE_READ_DATA


def suspended_without_breakaway():
    return CREATE_SUSPENDED, CREATE_BREAKAWAY_FROM_JOB


def _missing(handle):
    if handle in (0, None):
        return True
    return int(handle) in {0, _INVALID, _INVALID & 0xFFFFFFFFFFFFFFFF}


def _last_error(ctypes):
    get = getattr(ctypes, "get_last_error", None)
    return 0 if get is None else get()


def _win_error(ctypes, code=None):
    if code is None:
        code = _last_error(ctypes)
    factory = getattr(ctypes, "WinError", None)
    return OSError(code) if factory is None else factory(code)


def _bind(kernel, ctypes):
    """HANDLE 用 c_void_p。默认 c_int 会截断高 32 位。"""
    handle = ctypes.c_void_p
    dword = ctypes.c_uint32
    boolean = ctypes.c_int
    wide = ctypes.c_wchar_p
    void = ctypes.c_void_p
    pdword = ctypes.POINTER(dword)
    phandle = ctypes.POINTER(handle)
    specs = {
        "GetFileAttributesW": ([wide], dword),
        "CreateFileW": ([wide, dword, dword, void, dword, dword, handle], handle),
        "ReadFile": ([handle, void, dword, pdword, void], boolean),
        "WriteFile": ([handle, void, dword, pdword, void], boolean),
        "FlushFileBuffers": ([handle], boolean),
        "CloseHandle": ([handle], boolean),
        "CreatePipe": ([phandle, phandle, void, dword], boolean),
        "CreateProcessW": (
            [wide, wide, void, void, boolean, dword, void, wide, void, void],
            boolean,
        ),
        "CreateJobObjectW": ([void, wide], handle),
        "SetInformationJobObject": ([handle, dword, void, dword], boolean),
        "AssignProcessToJobObject": ([handle, handle], boolean),
        "ResumeThread": ([handle], dword),
        "WaitForSingleObject": ([handle, dword], dword),
        "GetExitCodeProcess": ([handle, pdword], boolean),
        "QueryInformationJobObject": ([handle, dword, void, dword, pdword], boolean),
        "TerminateProcess": ([handle, dword], boolean),
        "IsProcessInJob": ([handle, handle, ctypes.POINTER(boolean)], boolean),
        "OpenProcess": ([dword, boolean, dword], handle),
        "GetCurrentProcess": ([], handle),
    }
    for name, (args, result) in specs.items():
        function = getattr(kernel, name, None)
        if function is None:
            continue
        function.argtypes = args
        function.restype = result
