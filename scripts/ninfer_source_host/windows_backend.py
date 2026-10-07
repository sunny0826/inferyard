"""Windows 进程后端。导入本模块不加载 WinDLL，也不读取待执行文件。"""

from __future__ import annotations

import re
import sys

from .windows_backend_limits import (
    ACTIVE_PROCESS_LIMIT,
    BREAKAWAY_FLAGS,
    CREATE_BREAKAWAY_FROM_JOB,
    CREATE_SUSPENDED,
    JOB_LIMIT_FLAGS,
    MAX_UTF16_UNITS,
    PUMP_MAX,
    RETAIN_MAX,
    WAIT_OBJECT_0,
    WAIT_TIMEOUT,
)

_SHA = re.compile(r"[0-9a-f]{64}")
_SPEC_FIELDS = frozenset({"argv", "cwd", "executable_sha256", "script_sha256", "no_descendants"})


class ProcessRef:
    """一次 start 创建的原进程、主线程、Job 与管道句柄。不能用数字 PID 重建。"""

    def __init__(self, process, thread, stdout, stderr, extra=(), unknown=(), resources=None):
        self.process = process
        self.thread = thread
        self.stdout = stdout
        self.stderr = stderr
        self.job = None
        self.extra = tuple(extra)
        self.unknown = tuple(unknown)
        self.resources = resources
        self.held_stdout = b""
        self.held_stderr = b""
        self.pipes_attempted = False
        self.handle_attempted = False

    def process_handles(self):
        if self.process is None:
            return ()
        pending = [self.process, self.thread]
        if self.job is not None:
            pending.append(self.job)
        return tuple(pending)


class WindowsBackend:
    """冻结的七个 Backend 方法。注入 api 只用于模拟；省略 api 时仅 Windows 可构造。"""

    def __init__(self, api=None, *, errors=None):
        self._errors = errors
        if api is not None:
            self._api = api
            return
        if sys.platform != "win32":
            self._raise_backend()
        from .windows_backend_winapi import RealWindowsApi

        self._api = RealWindowsApi()

    def start(self, spec: dict) -> object:
        self._validate_spec(spec)
        if CREATE_SUSPENDED & CREATE_BREAKAWAY_FROM_JOB or JOB_LIMIT_FLAGS & BREAKAWAY_FLAGS:
            self._raise_host("host_job")
        if JOB_LIMIT_FLAGS != 0x8 or ACTIVE_PROCESS_LIMIT != 1:
            self._raise_host("host_job")
        try:
            spawned = self._api.spawn_suspended(spec["argv"], spec["cwd"], CREATE_SUSPENDED)
        except Exception as exc:
            self._fail_spawn(exc)
        ref = ProcessRef(
            spawned.process,
            spawned.thread,
            spawned.stdout,
            spawned.stderr,
            getattr(spawned, "extra", ()),
        )
        try:
            ref.job = self._api.create_job(JOB_LIMIT_FLAGS, ACTIVE_PROCESS_LIMIT)
            self._api.assign_job(ref.job, ref.process)
            self._api.resume(ref.thread)
        except Exception as exc:
            self._keep_unknown(ref, exc)
            self._raise_start(ref, exc)
        return ref

    def pump(self, ref: object, limit: int) -> dict:
        if type(limit) is not int or not 0 < limit <= PUMP_MAX:
            self._raise_host("host_output_limit")
        try:
            stdout, stderr = ref.stdout, ref.stderr
        except AttributeError as exc:
            self._raise_backend(exc)
        out = self._emit_pending(ref, "stdout", limit)
        err = self._emit_pending(ref, "stderr", limit)
        fresh_out = b""
        try:
            out_eof = self._take(ref, "stdout", stdout, limit - len(out))
            fresh_out = self._emit_pending(ref, "stdout", limit - len(out))
            err_eof = self._take(ref, "stderr", stderr, limit - len(err))
        except Exception:
            ref.held_stdout = out + fresh_out + ref.held_stdout
            ref.held_stderr = err + ref.held_stderr
            raise
        out += fresh_out
        err += self._emit_pending(ref, "stderr", limit - len(err))
        return {
            "stdout": out,
            "stderr": err,
            "eof": out_eof and err_eof and not ref.held_stdout and not ref.held_stderr,
        }

    def exit_code(self, ref: object) -> int | None:
        try:
            process = ref.process
        except AttributeError as exc:
            self._raise_backend(exc)
        if process is None:
            self._raise_backend()
        try:
            status = self._api.wait_zero(process)
        except Exception as exc:
            self._raise_backend(exc)
        if type(status) is not int:
            self._raise_backend()
        if status == WAIT_TIMEOUT:
            return None
        if status != WAIT_OBJECT_0:
            self._raise_backend()
        try:
            code = self._api.get_exit_code(process)
        except Exception as exc:
            self._raise_backend(exc)
        if type(code) is not int:
            self._raise_backend()
        return code

    def terminate(self, ref: object) -> None:
        try:
            process = ref.process
        except AttributeError as exc:
            self._raise_backend(exc)
        if process is None:
            self._raise_backend()
        try:
            self._api.terminate(process)
        except Exception as exc:
            self._raise_backend(exc)

    def kill(self, ref: object) -> None:
        try:
            process = ref.process
        except AttributeError as exc:
            self._raise_backend(exc)
        if process is None:
            self._raise_backend()
        try:
            self._api.kill(process)
        except Exception as exc:
            self._raise_backend(exc)

    def close_pipes(self, ref: object) -> None:
        try:
            attempted = ref.pipes_attempted
            handles = (ref.stdout, ref.stderr)
        except AttributeError as exc:
            self._raise_backend(exc)
        if attempted:
            self._raise_backend()
        ref.pipes_attempted = True
        if ref.process is None:
            return
        self._close_once(handles)

    def close_handle(self, ref: object) -> None:
        try:
            attempted = ref.handle_attempted
        except AttributeError as exc:
            self._raise_backend(exc)
        if attempted:
            self._raise_backend()
        ref.handle_attempted = True
        pending = list(ref.process_handles())
        pending.extend(ref.extra)
        unknown = list(ref.unknown)
        for index, handle in enumerate(pending):
            if handle in unknown:
                continue
            try:
                self._api.close(handle)
            except Exception:
                unknown.append(handle)
                ref.extra = tuple(pending[index + 1 :])
                ref.unknown = tuple(unknown)
                self._raise_backend()
        ref.extra = ()
        ref.unknown = tuple(unknown)
        if ref.unknown:
            self._raise_backend()

    def _emit_pending(self, ref, name, limit):
        """held 是唯一缓冲。先交出已保留字节，调用方只为本轮剩余额度读取。"""
        if limit <= 0:
            return b""
        attr = self._held_name(name)
        held = getattr(ref, attr)
        delivered = held[:limit]
        setattr(ref, attr, held[limit:])
        return delivered

    def _take(self, ref, name, pipe, limit):
        pending = len(getattr(ref, self._held_name(name)))
        if limit < 0 or pending >= RETAIN_MAX:
            return False
        if limit == 0:
            data, eof = self._read_new(ref, name, pipe, 0)
            return eof and not data
        room = min(limit, RETAIN_MAX - pending)
        data, eof = self._read_new(ref, name, pipe, room)
        if data:
            attr = self._held_name(name)
            setattr(ref, attr, getattr(ref, attr) + data)
        return eof

    def _read_new(self, ref, name, pipe, limit):
        try:
            peeked = self._api.peek(pipe)
        except Exception as exc:
            self._raise_backend(exc)
        if type(peeked) is not tuple or len(peeked) != 2:
            self._raise_backend()
        available, eof = peeked
        if type(available) is not int or available < 0 or type(eof) is not bool:
            self._raise_backend()
        size = min(available, limit)
        if size == 0:
            return b"", eof and limit > 0
        try:
            data = self._api.read(pipe, size)
        except Exception as exc:
            self._raise_backend(exc)
        if type(data) is not bytes or len(data) != size:
            if type(data) is bytes:
                attr = self._held_name(name)
                current = getattr(ref, attr)
                room = RETAIN_MAX - len(current)
                if room > 0:
                    setattr(ref, attr, current + data[:room])
            self._raise_backend()
        return data, eof and available <= limit

    def _held_name(self, name):
        if name == "stdout":
            return "held_stdout"
        return "held_stderr"

    def _close_once(self, handles):
        for handle in handles:
            try:
                self._api.close(handle)
            except Exception as exc:
                self._raise_backend(exc)

    def _validate_spec(self, spec):
        if type(spec) is not dict or set(spec) != _SPEC_FIELDS:
            self._raise_host("host_job")
        self._validate_argv(spec["argv"])
        self._validate_cwd(spec["cwd"])
        for name in ("executable_sha256", "script_sha256"):
            if type(spec[name]) is not str or _SHA.fullmatch(spec[name]) is None:
                self._raise_host("host_job")
        if spec["no_descendants"] is not True:
            self._raise_host("host_job")

    def _validate_argv(self, argv):
        if type(argv) is not tuple or not argv:
            self._raise_host("host_job")
        total = 0
        for arg in argv:
            if type(arg) is not str or "\0" in arg:
                self._raise_host("host_job")
            try:
                total += len(arg.encode("utf-16-le")) // 2
            except UnicodeEncodeError:
                self._raise_host("host_job")
            if total > MAX_UTF16_UNITS:
                self._raise_host("host_job")

    def _validate_cwd(self, cwd):
        if type(cwd) is not str or not _windows_absolute(cwd):
            self._raise_host("host_job")

    def _fail_spawn(self, exc):
        host, backend, start = self._types()
        if isinstance(exc, start):
            raise exc
        spawned = getattr(exc, "spawned", None)
        if spawned is not None and getattr(spawned, "process", None) is not None:
            ref = ProcessRef(
                spawned.process,
                spawned.thread,
                spawned.stdout,
                spawned.stderr,
                getattr(exc, "extra", ()),
                getattr(exc, "unknown", ()),
            )
            self._raise_start(ref, exc)
        resources = getattr(exc, "resources", None)
        if resources is not None:
            ref = ProcessRef(None, None, None, None, (), (), resources)
            self._keep_pipe_cleanup(ref, exc)
            self._raise_start(ref, exc)
        if isinstance(exc, host) and not isinstance(exc, backend):
            raise exc
        self._raise_start(None, exc)

    def _keep_pipe_cleanup(self, ref, exc):
        unknown = []
        close_error = None
        for item in getattr(exc, "closes", ()):
            if not item.ok and item.handle not in unknown:
                unknown.append(item.handle)
            if close_error is None and getattr(item, "error", None) is not None:
                close_error = item.error
        pending = getattr(exc.resources, "pending", ())
        ref.extra = tuple(handle for handle in pending if handle not in unknown)
        ref.unknown = tuple(unknown)

    def _keep_unknown(self, ref, exc):
        found = list(ref.unknown)
        found.extend(getattr(exc, "unknown_handles", ()))
        leaked = getattr(exc, "leaked_job", None)
        if leaked is not None and leaked not in found:
            found.append(leaked)
        ref.unknown = tuple(found)

    def _raise_start(self, ref, cause):
        _host, _backend, start = self._types()
        raise start(ref, cause) from cause

    def _raise_backend(self, cause=None):
        _host, backend, _start = self._types()
        if cause is None:
            raise backend()
        raise backend() from cause

    def _raise_host(self, code):
        host, _backend, _start = self._types()
        raise host(code)

    def _types(self):
        if self._errors is not None:
            return self._errors
        from .custody import BackendError, BackendStartError, HostError

        self._errors = (HostError, BackendError, BackendStartError)
        return self._errors


def _windows_absolute(path):
    if "\0" in path or "/" in path or path.startswith("\\\\?\\") or path.startswith("\\\\.\\"):
        return False
    if len(path) >= 3 and path[0].isalpha() and path[1:3] == ":\\":
        rest = path[3:]
        return rest == "" or all(part not in {"", ".", ".."} for part in rest.split("\\"))
    if not path.startswith("\\\\"):
        return False
    parts = path[2:].split("\\")
    return len(parts) >= 2 and all(part not in {"", ".", ".."} for part in parts)
