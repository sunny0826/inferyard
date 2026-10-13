"""Read-only token identity and session queries; raw identities remain internal."""

import ctypes
from ctypes import wintypes

from inferyard.platforms.windows_api import _error, _function, _kernel


def owner_session(pid):
    kernel = _kernel()
    security = ctypes.WinDLL("advapi32", use_last_error=True)
    open_process = _function(
        kernel, "OpenProcess", [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD], wintypes.HANDLE
    )
    close = _function(kernel, "CloseHandle", [wintypes.HANDLE])
    open_token = _function(
        security,
        "OpenProcessToken",
        [wintypes.HANDLE, wintypes.DWORD, ctypes.POINTER(wintypes.HANDLE)],
    )
    token_info = _function(
        security,
        "GetTokenInformation",
        [
            wintypes.HANDLE,
            ctypes.c_int,
            ctypes.c_void_p,
            wintypes.DWORD,
            ctypes.POINTER(wintypes.DWORD),
        ],
    )
    sid_length = _function(security, "GetLengthSid", [ctypes.c_void_p], wintypes.DWORD)
    session_id = _function(
        kernel, "ProcessIdToSessionId", [wintypes.DWORD, ctypes.POINTER(wintypes.DWORD)]
    )
    handle = open_process(0x1000, False, pid)
    if not handle:
        raise _error()
    token = wintypes.HANDLE()
    try:
        if not open_token(handle, 0x0008, ctypes.byref(token)):  # TOKEN_QUERY
            raise _error()
        size = wintypes.DWORD()
        token_info(token, 1, None, 0, ctypes.byref(size))  # TokenUser
        if not 0 < size.value <= 65536:
            raise OSError("token_size_unavailable")
        buffer = ctypes.create_string_buffer(size.value)
        if not token_info(token, 1, buffer, size, ctypes.byref(size)):
            raise _error()
        sid = ctypes.cast(buffer, ctypes.POINTER(ctypes.c_void_p))[0]
        length = sid_length(sid)
        if not 0 < length <= 68:
            raise OSError("sid_unavailable")
        owner = ctypes.string_at(sid, length).hex()
        session = wintypes.DWORD()
        if not session_id(pid, ctypes.byref(session)):
            raise _error()
        return owner, session.value
    finally:
        if token:
            close(token)
        close(handle)
