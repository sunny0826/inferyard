"""System-wide Windows lock location, independent of user environment variables."""

import ctypes
from ctypes import wintypes
from pathlib import Path


def common_data_root():
    # CSIDL_COMMON_APPDATA is the system's ProgramData known folder, including
    # installations with a relocated system drive. Never fall back to user temp.
    library = ctypes.WinDLL("shell32", use_last_error=True)
    query = library.SHGetFolderPathW
    query.argtypes = [wintypes.HWND, ctypes.c_int, wintypes.HANDLE, wintypes.DWORD, wintypes.LPWSTR]
    query.restype = ctypes.c_long
    result = ctypes.create_unicode_buffer(260)
    if query(None, 0x0023, None, 0, result) != 0 or not result.value:
        raise OSError("windows_common_data_unavailable")
    return Path(result.value)
