"""Stage46 文件适配器。默认真实路径只在 Windows 构造；注入对象单独标为 mock。"""

from __future__ import annotations

from .file_io_codec import IoError
from .probe_io_limits import (
    ERROR_FILE_NOT_FOUND,
    ERROR_PATH_NOT_FOUND,
    FILE_ATTRIBUTE_NORMAL,
    FILE_ATTRIBUTE_REPARSE_POINT,
    FILE_READ_DATA,
    FILE_ROOT,
)
from .probe_io_path import contained_ancestors, path_relation, require_root

__all__ = ["ProbeFileApi"]

_READ_ONLY = "read_only"


class ProbeFileApi:
    """供 execute_io 注入的普通文件根。root 必须是固定 files 根，不能从请求路径派生。"""

    def __init__(self, root: str, native=None):
        self.root = require_root(root)
        self.native = native
        self.injected = native is not None
        self.calls = []
        self._open = {}
        self._gate = None
        self._close_unknown = set()
        self._close_skipped = set()

    def is_reparse(self, path):
        self._record("is_reparse", path)
        relation = path_relation(path, self.root)
        if relation == "outside":
            raise IoError("io_path")
        try:
            if relation == "ancestor":
                return self._ancestor_reparse(path)
            if relation == "root":
                return self._present_reparse(path)
            return self._inside_reparse(path)
        except _Coded as exc:
            raise IoError(exc.io_code) from exc

    def open_read(self, path):
        self._record("open_read", path)
        self._open_leaf(path, missing_ok=False)
        self._guard_work()
        try:
            handle = self._native().open_existing(path, FILE_READ_DATA, FILE_ATTRIBUTE_NORMAL)
        except BaseException:
            self._guard_after()
            raise
        self._open[self._key(handle)] = _READ_ONLY
        self._guard_after()
        return handle

    def create_new(self, path):
        self._record("create_new", path)
        self._open_leaf(path, missing_ok=True)
        self._guard_work()
        try:
            handle = self._native().create_new(path)
        except BaseException:
            self._guard_after()
            raise
        self._open[self._key(handle)] = "write"
        self._guard_after()
        return handle

    def read(self, handle, size):
        self._record("read", handle, size)
        self._known(handle)
        if type(size) is not int or isinstance(size, bool) or size < 0:
            raise IoError("io_request")
        self._guard_work()
        try:
            data = self._native().read(handle, size)
        except BaseException:
            self._guard_after()
            raise
        self._guard_after()
        if type(data) is not bytes:
            raise IoError("io_failure")
        return data

    def write(self, handle, data):
        self._record("write", handle, data)
        self._writable(handle)
        if type(data) is not bytes:
            raise IoError("io_request")
        self._guard_work()
        try:
            written = self._native().write(handle, data)
        except BaseException:
            self._guard_after()
            raise
        self._guard_after()
        if type(written) is not int or isinstance(written, bool):
            raise IoError("io_failure")
        return written

    def flush(self, handle):
        self._record("flush", handle)
        return self._finish(handle, "flush")

    def fsync(self, handle):
        self._record("fsync", handle)
        return self._finish(handle, "fsync")

    def bind_gate(self, gate):
        """诊断层绑定原期限。不改变 ProbeFileApi(root) 的构造签名。"""
        self._gate = gate

    def close(self, handle):
        self._record("close", handle)
        key = self._known(handle)
        if key in self._close_unknown or key in self._close_skipped:
            raise IoError("io_failure")
        if self._gate is not None:
            self._gate.after()
            if self._gate.blocked():
                self._close_skipped.add(key)
                from .probe_fixture_custody import DeadlineStop

                raise DeadlineStop("close_after_total")
        try:
            self._native().close(handle)
        except BaseException:
            self._close_unknown.add(key)
            raise
        self._open.pop(key, None)

    def _finish(self, handle, name):
        kind = self._open.get(self._key(handle))
        if kind is None:
            raise IoError("io_failure")
        if kind == _READ_ONLY:
            return {"api": name, "pending": False, "flush_file_buffers": False}
        native = self._native()
        method = getattr(native, "flush_file_buffers", None)
        if method is None:
            method = native.flush_buffers
        method(handle)
        return {"api": "FlushFileBuffers", "pending": False, "flush_file_buffers": True}

    def _open_leaf(self, path, *, missing_ok):
        chain = contained_ancestors(path, self.root)
        for item in chain[:-1]:
            if self._present_reparse(item):
                raise IoError("io_path")
        leaf = self._leaf_state(chain[-1])
        if leaf == "reparse":
            raise IoError("io_path")
        if leaf == "missing" and not missing_ok:
            raise IoError("io_failure")
        if leaf == "present" and missing_ok:
            raise FileExistsError(path)
        if leaf == "ancestor_missing":
            raise IoError("io_path")

    def _inside_reparse(self, path):
        chain = contained_ancestors(path, self.root)
        for item in chain[:-1]:
            if self._present_reparse(item):
                raise IoError("io_path")
        return self._leaf_state(chain[-1]) == "reparse"

    def _ancestor_reparse(self, path):
        """许可根之外的祖先只做真实属性查询，不能被词法预检恒判为普通目录。"""
        try:
            flags = self._native_attributes(path)
        except OSError as exc:
            raise IoError("io_failure") from exc
        if type(flags) is not int or isinstance(flags, bool):
            raise IoError("io_failure")
        return bool(flags & FILE_ATTRIBUTE_REPARSE_POINT)

    def _present_reparse(self, path):
        flags = self._attributes(path, missing="error")
        return bool(flags & FILE_ATTRIBUTE_REPARSE_POINT)

    def _leaf_state(self, path):
        try:
            flags = self._native_attributes(path)
        except OSError as exc:
            code = _windows_code(exc)
            if code == ERROR_FILE_NOT_FOUND:
                return "missing"
            if code == ERROR_PATH_NOT_FOUND:
                return "ancestor_missing"
            raise _Coded("io_failure") from exc
        if type(flags) is not int or isinstance(flags, bool):
            raise _Coded("io_failure")
        if flags & FILE_ATTRIBUTE_REPARSE_POINT:
            return "reparse"
        return "present"

    def _attributes(self, path, *, missing):
        _ = missing
        try:
            flags = self._native_attributes(path)
        except OSError as exc:
            code = _windows_code(exc)
            reason = "io_path" if code == ERROR_PATH_NOT_FOUND else "io_failure"
            raise _Coded(reason) from exc
        if type(flags) is not int or isinstance(flags, bool):
            raise _Coded("io_failure")
        return flags

    def _native_attributes(self, path):
        self._guard_work()
        try:
            flags = self._native().attributes(path)
        except BaseException:
            self._guard_after()
            raise
        self._guard_after()
        return flags

    def _guard_work(self):
        if self._gate is None:
            return
        if not self._gate.work_open():
            from .probe_fixture_custody import DeadlineStop

            raise DeadlineStop("file_work")

    def _guard_after(self):
        if self._gate is not None:
            self._gate.after()

    def _native(self):
        if self.native is None:
            from .probe_io_winapi import RealProbeFileApi

            self.native = RealProbeFileApi()
            self.injected = False
        return self.native

    def _known(self, handle):
        key = self._key(handle)
        if key not in self._open:
            raise IoError("io_failure")
        return key

    def _writable(self, handle):
        if self._open.get(self._known(handle)) != "write":
            raise IoError("io_failure")

    def _record(self, name, *args):
        self.calls.append((name, *args))

    def _key(self, handle):
        if isinstance(handle, int) and not isinstance(handle, bool):
            return ("int", handle)
        return ("ref", id(handle))


class _Coded(OSError):
    def __init__(self, code):
        super().__init__(code)
        self.io_code = code


def _windows_code(error):
    code = getattr(error, "winerror", None)
    if type(code) is int and not isinstance(code, bool):
        return code
    args = getattr(error, "args", ())
    if args and type(args[0]) is int and not isinstance(args[0], bool):
        return args[0]
    return None


def fixed_root():
    return FILE_ROOT


def attribute_probe_flags():
    """目录 reparse 探测使用的访问和打开标志。不调用 WinAPI。"""
    from .probe_io_limits import (
        FILE_FLAG_BACKUP_SEMANTICS,
        FILE_FLAG_OPEN_REPARSE_POINT,
        FILE_READ_ATTRIBUTES,
    )

    return FILE_READ_ATTRIBUTES, FILE_FLAG_OPEN_REPARSE_POINT | FILE_FLAG_BACKUP_SEMANTICS
