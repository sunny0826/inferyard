"""诊断层文件代理。库的 close 没有 total 检查，句柄留在这里交给调用方。"""

from __future__ import annotations

from .probe_fixture_custody import DeadlineStop


class FileHold(Exception):
    """原文件句柄还在。native、api 和 resources 都是当初那一个对象。"""

    def __init__(self, native, api, resources, detail):
        super().__init__("fixture_file_unknown")
        self.native = native
        self.api = api
        self.resources = resources
        self.detail = detail


class _Opened:
    def __init__(self, value):
        self.value = value
        self.attempted = False
        self.closed = False
        self.unknown = False
        self.error = None


class _Ledger:
    def __init__(self):
        self.handles = []

    def keep(self, value):
        found = self.find(value)
        if found is None:
            found = _Opened(value)
            self.handles.append(found)
        return found

    def find(self, value):
        for item in self.handles:
            if item.value == value:
                return item
        return None

    def live(self):
        return [item for item in self.handles if not item.closed]


class FileGate:
    """包住 execute_io 看到的 io_api。每个方法返回后采样，下一次开始前再看时钟。"""

    def __init__(self, api, gate):
        self.api = api
        self.gate = gate
        self.resources = _Ledger()
        native = getattr(api, "native", None)
        self.native = native if native is not None else api

    def is_reparse(self, path):
        self._work()
        value = self._call(lambda: self.api.is_reparse(path))
        self._sample()
        return value

    def open_read(self, path):
        self._work()
        handle = self._call(lambda: self.api.open_read(path))
        return self._accept(handle)

    def create_new(self, path):
        self._work()
        handle = self._call(lambda: self.api.create_new(path))
        return self._accept(handle)

    def read(self, handle, size):
        self._work()
        data = self._call(lambda: self.api.read(handle, size))
        self._sample()
        return data

    def write(self, handle, data):
        self._work()
        written = self._call(lambda: self.api.write(handle, data))
        self._sample()
        return written

    def flush(self, handle):
        return self._finish(handle, "flush")

    def fsync(self, handle):
        return self._finish(handle, "fsync")

    def close(self, handle):
        opened = self.resources.keep(handle)
        if opened.attempted or opened.closed:
            from .file_io_codec import IoError

            raise IoError("io_failure")
        self._sample()
        if self.gate.blocked():
            raise DeadlineStop("close_after_total")
        opened.attempted = True
        try:
            self.api.close(handle)
        except BaseException as exc:
            opened.unknown = True
            opened.error = exc
            self._keep_error(exc)
            raise
        opened.closed = True
        self._sample()

    def live(self):
        return bool(self.resources.live())

    def hold(self, result):
        detail = _detail(result, self.resources)
        exc = FileHold(self._subject(), self.api, self.resources, detail)
        self.attach(exc)
        return exc

    def attach(self, exc):
        if getattr(exc, "resources", None) is None:
            exc.resources = self.resources
        if getattr(exc, "api", None) is None:
            exc.api = self.api
        subject = self._subject()
        if getattr(exc, "native", None) is None:
            exc.native = subject
        subject.retained = self.resources
        self.api.retained = self.resources

    def _subject(self):
        native = getattr(self.api, "native", None)
        return native if native is not None else self.native

    def _work(self):
        if not self.gate.work_open():
            raise DeadlineStop("file_work")

    def _finish(self, handle, name):
        self._work()
        value = self._call(lambda: getattr(self.api, name)(handle))
        self._sample()
        return value

    def _accept(self, handle):
        self.resources.keep(handle)
        self._sample()
        return handle

    def _call(self, action):
        try:
            return action()
        except BaseException as exc:
            self._adopt_api()
            self._keep_error(exc)
            raise

    def _sample(self):
        try:
            self.gate.after()
        except BaseException as exc:
            self._keep_error(exc)
            raise

    def _adopt_api(self):
        opened = getattr(self.api, "_open", None)
        if type(opened) is not dict:
            return
        for key in opened:
            if type(key) is tuple and key and key[0] == "int" and type(key[1]) is int:
                self.resources.keep(key[1])

    def _keep_error(self, exc):
        if not self.gate.sticky and not isinstance(exc, (KeyboardInterrupt, SystemExit)):
            self.gate.after()
        self.attach(exc)


def _detail(result, resources):
    primary = result.get("primary_error") if type(result) is dict else None
    cleanup = result.get("cleanup_errors") if type(result) is dict else None
    return {
        "primary_error": primary,
        "cleanup_errors": list(cleanup or []),
        "handles": [item.value for item in resources.handles],
        "unknown": [item.value for item in resources.handles if item.unknown],
        "unattempted": [
            item.value for item in resources.handles if not item.attempted and not item.closed
        ],
    }
