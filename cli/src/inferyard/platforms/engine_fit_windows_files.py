"""Uniform native Windows file stamps; never mix Python stat/fstat semantics."""

import ctypes
import hashlib
import os
import stat
from ctypes import wintypes
from pathlib import Path

from inferyard.platforms.identity import PreflightError
from inferyard.platforms.windows_api import _error, _function, _kernel, open_file


class _BasicInfo(ctypes.Structure):
    _fields_ = [
        ("creation", ctypes.c_int64),
        ("access", ctypes.c_int64),
        ("write", ctypes.c_int64),
        ("change", ctypes.c_int64),
        ("attributes", ctypes.c_uint32),
    ]


class _StandardInfo(ctypes.Structure):
    _fields_ = [
        ("allocation", ctypes.c_int64),
        ("size", ctypes.c_int64),
        ("links", ctypes.c_uint32),
        ("delete_pending", ctypes.c_ubyte),
        ("directory", ctypes.c_ubyte),
    ]


class _IdInfo(ctypes.Structure):
    _fields_ = [("volume", ctypes.c_uint64), ("identifier", ctypes.c_ubyte * 16)]


def descriptor_stamp(descriptor):
    import msvcrt

    handle = msvcrt.get_osfhandle(descriptor)
    library = _kernel()
    get_type = _function(library, "GetFileType", [wintypes.HANDLE], ctypes.c_uint32)
    if get_type(handle) != 1:  # FILE_TYPE_DISK
        raise PreflightError("engine_fit_not_regular_file")
    query = _function(
        library,
        "GetFileInformationByHandleEx",
        [wintypes.HANDLE, ctypes.c_int, ctypes.c_void_p, ctypes.c_uint32],
    )
    basic, standard, identity = _BasicInfo(), _StandardInfo(), _IdInfo()
    for kind, info in ((0, basic), (1, standard), (18, identity)):
        if not query(handle, kind, ctypes.byref(info), ctypes.sizeof(info)):
            raise _error()
    if basic.attributes & (0x10 | 0x400) or standard.directory or standard.size < 0:
        raise PreflightError("engine_fit_not_regular_file")
    if standard.delete_pending:
        raise PreflightError("engine_fit_file_changed")
    # FileIdInfo retains the full 128-bit ID; BasicInfo uses actual ChangeTime.
    # Access time can change because of this read and is intentionally excluded.
    return (
        identity.volume,
        bytes(identity.identifier),
        basic.attributes,
        standard.size,
        basic.creation,
        basic.write,
        basic.change,
    )


def path_stamp(path):
    descriptor = open_file(Path(path), os.O_RDONLY)
    try:
        return descriptor_stamp(descriptor)
    finally:
        os.close(descriptor)


def file_hash(path):
    from inferyard.platforms.engine_fit import _stamp

    path = Path(path)
    before, link = path.stat(), path.lstat()
    if not stat.S_ISREG(before.st_mode):
        raise PreflightError("engine_fit_not_regular_file")
    resolved = path.resolve(strict=True)
    expected = path_stamp(resolved)
    descriptor = open_file(resolved, os.O_RDONLY)
    with os.fdopen(descriptor, "rb") as stream:
        if descriptor_stamp(stream.fileno()) != expected:
            raise PreflightError("engine_fit_file_changed")
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
        if descriptor_stamp(stream.fileno()) != expected:
            raise PreflightError("engine_fit_file_changed")
    if (
        path_stamp(path.resolve(strict=True)) != expected
        or _stamp(path.stat()) != _stamp(before)
        or _stamp(path.lstat()) != _stamp(link)
    ):
        raise PreflightError("engine_fit_file_changed")
    return digest, expected[3]
