"""同进程接手。活句柄或未知关闭留在异常上，调用方保住同一个对象。"""

from __future__ import annotations


class DeadlineStop(Exception):
    """这次调用已经越过原期限。不是坏时钟，不能把后续收尾标成粘滞。"""

    def __init__(self, observed):
        super().__init__(observed)
        self.observed = observed


class PipeHold(Exception):
    """管道两端尚未确认释放。resources 是当初那一个 _Pipe，不是副本。"""

    def __init__(self, native, resources, detail):
        super().__init__("fixture_pipe_unknown")
        self.native = native
        self.resources = resources
        self.detail = detail


class _Pipe:
    def __init__(self, native, read_handle, write_handle):
        self.native = native
        self.handles = {"read": read_handle, "write": write_handle}
        self.tried = {"read": False, "write": False}
        self.released = {"read": False, "write": False}
        self.errors = []

    def close_with_gate(self, gate):
        for name in ("write", "read"):
            if self.tried[name]:
                continue
            if gate is not None and gate.blocked():
                break
            self.tried[name] = True
            try:
                self.native.close(self.handles[name])
            except BaseException as exc:
                self.released[name] = False
                self.errors.append({"name": name, "handle": self.handles[name], "error": exc})
                if gate is not None:
                    gate.after()
                if isinstance(exc, (KeyboardInterrupt, SystemExit)):
                    raise
            else:
                self.released[name] = True
            if gate is not None:
                gate.after()

    def fully_released(self):
        return self.released["write"] and self.released["read"]
