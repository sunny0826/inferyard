"""Small Windows APIs with explicit handle ownership and exact process creation identity."""

import ctypes
import os
from ctypes import wintypes


def _kernel():
    return ctypes.WinDLL("kernel32", use_last_error=True)


def _function(library, name, arguments, result=wintypes.BOOL):
    function = getattr(library, name)
    function.argtypes = arguments
    function.restype = result
    return function


def _error():
    return ctypes.WinError(ctypes.get_last_error())


def _long_path(path):
    value = str(path.absolute())
    if value.startswith("\\\\?\\"):
        return value
    if value.startswith("\\\\"):
        return "\\\\?\\UNC\\" + value[2:]
    return "\\\\?\\" + value


def process_start(pid):
    library = _kernel()
    open_process = _function(
        library, "OpenProcess", [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD], wintypes.HANDLE
    )
    close = _function(library, "CloseHandle", [wintypes.HANDLE])
    get_times = _function(
        library, "GetProcessTimes", [wintypes.HANDLE] + [ctypes.POINTER(wintypes.FILETIME)] * 4
    )
    handle = open_process(0x1000, False, pid)  # PROCESS_QUERY_LIMITED_INFORMATION
    if not handle:
        raise _error()
    try:
        values = [wintypes.FILETIME() for _ in range(4)]
        if not get_times(handle, *(ctypes.byref(value) for value in values)):
            raise _error()
        return (values[0].dwHighDateTime << 32) | values[0].dwLowDateTime
    finally:
        close(handle)


class _FileInfo(ctypes.Structure):
    _fields_ = [
        ("attributes", wintypes.DWORD),
        ("creation", wintypes.FILETIME),
        ("access", wintypes.FILETIME),
        ("write", wintypes.FILETIME),
        ("volume", wintypes.DWORD),
        ("size_high", wintypes.DWORD),
        ("size_low", wintypes.DWORD),
        ("links", wintypes.DWORD),
        ("index_high", wintypes.DWORD),
        ("index_low", wintypes.DWORD),
    ]


def open_file(path, flags):
    import msvcrt

    library = _kernel()
    create = _function(
        library,
        "CreateFileW",
        [
            wintypes.LPCWSTR,
            wintypes.DWORD,
            wintypes.DWORD,
            ctypes.c_void_p,
            wintypes.DWORD,
            wintypes.DWORD,
            wintypes.HANDLE,
        ],
        wintypes.HANDLE,
    )
    close = _function(library, "CloseHandle", [wintypes.HANDLE])
    inspect = _function(
        library, "GetFileInformationByHandle", [wintypes.HANDLE, ctypes.POINTER(_FileInfo)]
    )
    access = 0x80000000  # GENERIC_READ
    if flags & os.O_RDWR:
        access |= 0x40000000
    elif flags & os.O_WRONLY:
        access = 0x40000000
    disposition = 1 if flags & os.O_EXCL else 4 if flags & os.O_CREAT else 3
    # FILE_SHARE_READ|WRITE|DELETE, FILE_FLAG_OPEN_REPARSE_POINT|BACKUP_SEMANTICS.
    handle = create(_long_path(path), access, 7, None, disposition, 0x02200000, None)
    if handle == ctypes.c_void_p(-1).value:
        raise _error()
    try:
        info = _FileInfo()
        if not inspect(handle, ctypes.byref(info)):
            raise _error()
        if info.attributes & 0x400:  # FILE_ATTRIBUTE_REPARSE_POINT
            raise OSError("reparse_point_rejected")
        fd = msvcrt.open_osfhandle(handle, (flags & (os.O_WRONLY | os.O_RDWR)) | os.O_BINARY)
    except BaseException:
        close(handle)
        raise
    return fd  # The CRT descriptor now owns the handle.


def move_file(temporary, path, overwrite):
    function = _function(
        _kernel(), "MoveFileExW", [wintypes.LPCWSTR, wintypes.LPCWSTR, wintypes.DWORD]
    )
    if not function(_long_path(temporary), _long_path(path), 8 | int(overwrite)):
        raise _error()
