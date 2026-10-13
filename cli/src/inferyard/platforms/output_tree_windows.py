"""Windows directory anchors deny write/delete sharing for the entire path chain.

CreateFileW OPEN_REPARSE_POINT opens the link itself; handle attributes reject it.
Omitting FILE_SHARE_WRITE/DELETE prevents reparse mutation and rename while held.
https://learn.microsoft.com/windows/win32/api/fileapi/nf-fileapi-createfilew
"""

import ctypes
import os
from ctypes import wintypes
from pathlib import Path

from inferyard.platforms.output_tree import ChangedDirectory
from inferyard.platforms.windows_api import _long_path


class FileInformation(ctypes.Structure):
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


class WindowsTree:
    def __init__(self):
        self.handles = []
        self.paths = {}
        self.api = ctypes.WinDLL("kernel32", use_last_error=True)
        self.api.CreateFileW.argtypes = [
            wintypes.LPCWSTR,
            wintypes.DWORD,
            wintypes.DWORD,
            ctypes.c_void_p,
            wintypes.DWORD,
            wintypes.DWORD,
            wintypes.HANDLE,
        ]
        self.api.CreateFileW.restype = wintypes.HANDLE
        self.api.GetFileInformationByHandle.argtypes = [
            wintypes.HANDLE,
            ctypes.POINTER(FileInformation),
        ]
        self.api.GetFileInformationByHandle.restype = wintypes.BOOL
        self.api.CloseHandle.argtypes = [wintypes.HANDLE]
        self.api.CloseHandle.restype = wintypes.BOOL

    def info(self, handle):
        info = FileInformation()
        if not self.api.GetFileInformationByHandle(handle, ctypes.byref(info)):
            raise ctypes.WinError(ctypes.get_last_error())
        return info

    def descend(self, parent, name, *, create=False, exclusive=False):
        path = self.paths[parent] / name if parent is not None else Path(name)
        path = Path(_long_path(path))
        if create:
            try:
                path.mkdir()
            except FileExistsError:
                if exclusive:
                    raise
        # FILE_READ_ATTRIBUTES, FILE_SHARE_READ, OPEN_EXISTING,
        # FILE_FLAG_BACKUP_SEMANTICS | FILE_FLAG_OPEN_REPARSE_POINT.
        handle = self.api.CreateFileW(str(path), 0x80, 1, None, 3, 0x02200000, None)
        if handle == ctypes.c_void_p(-1).value:
            raise ctypes.WinError(ctypes.get_last_error())
        self.handles.append(handle)
        info = self.info(handle)
        if info.attributes & 0x400 or not info.attributes & 0x10:
            raise ChangedDirectory("output reparse point or non-directory")
        self.paths[handle] = path
        return handle

    def identity(self, handle):
        info = self.info(handle)
        return info.volume, (info.index_high << 32) | info.index_low

    def check(self):
        # Each handle denies replacement and reparse writes until close().
        pass

    def file(self, parent, name):
        from inferyard.platforms.platform_io import open_nofollow

        return open_nofollow(self.paths[parent] / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL)

    def close(self):
        for handle in reversed(self.handles):
            self.api.CloseHandle(handle)
