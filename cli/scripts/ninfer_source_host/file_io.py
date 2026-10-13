"""文件 worker。全部文件操作都在 execute_io 内，经 io_api 注入。本波 main 无执行许可。"""

from __future__ import annotations

import base64
import hashlib
import json
import sys
from collections.abc import Callable

from .file_io_codec import (
    BODY_LIMIT,
    DECODED_ARG_LIMIT,
    IO_CODES,
    RESULT_FIELDS,
    RESULT_LIMIT,
    IoError,
    decode_write,
    lexical_ancestors,
    loads_strict,
    validate_io_request,
)

__all__ = ["execute_io", "main", "validate_io_request"]


class _Halt(Exception):
    pass


def execute_io(value: dict, *, io_api: object, now_ticks: Callable[[], int]) -> dict:
    try:
        request = validate_io_request(value)
    except IoError as exc:
        return _result(value, None, None, None, exc.code, [])
    return _Runner(request, io_api, now_ticks).run()


def main(argv: list[str] | None = None) -> int:
    """本波没有独立执行许可。合法请求也拒绝，不调用 execute_io。"""
    args = sys.argv if argv is None else argv
    if type(args) is not list or len(args) != 2 or type(args[1]) is not str:
        return 2
    try:
        raw = base64.b64decode(args[1], validate=True)
    except ValueError, TypeError:
        return 2
    if len(raw) > DECODED_ARG_LIMIT:
        return 2
    try:
        validate_io_request(loads_strict(raw.decode("utf-8")))
    except IoError, UnicodeError, ValueError:
        return 2
    return 2


class _Runner:
    def __init__(self, request, io_api, now_ticks):
        self.request = request
        self.io = io_api
        self.now = now_ticks
        self.primary = None
        self.cleanup = []
        self.handle = None
        self.closed = False
        self.last = None
        self.body = bytearray()
        self.byte_count = None
        self.digest = None

    def run(self):
        try:
            self._work()
        except _Halt:
            pass
        except IoError as exc:
            self._set(exc.code)
        except Exception:
            self._set("io_failure")
        self._close()
        if self.primary is None:
            self._note_deadline()
        data = None
        if self.primary is None and self.request["operation"] == "read":
            data = base64.b64encode(bytes(self.body)).decode("ascii")
            self._note_deadline()
            if self.primary is not None:
                data = None
        nbytes = self.byte_count if type(self.byte_count) is int else None
        result = _result(self.request, nbytes, self.digest, data, self.primary, self.cleanup)
        return self._seal(result)

    def _work(self):
        self._ensure_time()
        for item in lexical_ancestors(self.request["path"]):
            self._ensure_time()
            self._reparse(item)
        if self.request["operation"] == "write_new":
            self._write(decode_write(self.request))
        else:
            self._read()
        self._call(lambda: self.io.flush(self.handle))
        self._call(lambda: self.io.fsync(self.handle))

    def _write(self, raw):
        self._acquire(lambda: self.io.create_new(self.request["path"]))

        def accept(written, raw=raw):
            if type(written) is int and not isinstance(written, bool) and written >= 0:
                self.byte_count = written
            if type(written) is not int or isinstance(written, bool) or written != len(raw):
                self._set("io_failure")
                raise _Halt
            self.digest = hashlib.sha256(raw).hexdigest()

        self._call(lambda: self.io.write(self.handle, raw), accept)

    def _read(self):
        self._acquire(lambda: self.io.open_read(self.request["path"]))
        remaining = self.request["expected_bytes"]
        while remaining:
            size = min(BODY_LIMIT, remaining)

            def read_chunk(size=size):
                return self.io.read(self.handle, size)

            chunk = self._call(read_chunk, self._keep(size))
            if chunk == b"":
                break
            remaining -= len(chunk)
        self._call(lambda: self.io.read(self.handle, 1), self._finish_read)

    def _keep(self, size):
        def accept(chunk, size=size):
            if type(chunk) is not bytes or len(chunk) > size:
                self._set("io_failure")
                raise _Halt
            self.body.extend(chunk)
            self.byte_count = len(self.body)

        return accept

    def _finish_read(self, extra):
        if type(extra) is not bytes:
            self._set("io_failure")
            raise _Halt
        if extra:
            self.body.extend(extra)
            self.byte_count = len(self.body)
            self._set("io_failure")
            raise _Halt
        self.byte_count = len(self.body)
        self.digest = hashlib.sha256(bytes(self.body)).hexdigest()
        expected = self.request["expected_bytes"]
        if self.byte_count != expected or self.digest != self.request["expected_sha256"]:
            self._set("io_failure")
            raise _Halt

    def _reparse(self, item):
        try:
            hit = self.io.is_reparse(item)
        except IoError as exc:
            self._set(exc.code)
            raise _Halt from exc
        except Exception as exc:
            self._fail_io(exc)
            raise _Halt from exc
        if type(hit) is not bool:
            self._set("io_failure")
            raise _Halt
        if hit:
            self._set("io_path")
            raise _Halt

    def _acquire(self, action):
        self._ensure_time()
        try:
            handle = action()
        except _Halt:
            raise
        except IoError as exc:
            self._set(exc.code)
            raise _Halt from exc
        except Exception as exc:
            self._fail_io(exc)
            raise _Halt from exc
        self.handle = handle
        self._ensure_time()
        return handle

    def _call(self, action, accept=None):
        self._ensure_time()
        try:
            result = action()
        except _Halt:
            raise
        except IoError as exc:
            self._set(exc.code)
            raise _Halt from exc
        except Exception as exc:
            self._fail_io(exc)
            raise _Halt from exc
        if accept is not None:
            accept(result)
        self._ensure_time()
        return result

    def _seal(self, result):
        # 正文编码和结果 JSON 都在原 task 期限之内。跨期不再保留成功正文。
        self._note_deadline()
        if self.primary is None:
            return result
        if result["primary_error"] is None:
            result["primary_error"] = self.primary
            result["cleanup_errors"] = list(self.cleanup)
        else:
            for code in (self.primary, *self.cleanup):
                if code != result["primary_error"] and code not in result["cleanup_errors"]:
                    result["cleanup_errors"].append(code)
        result["data_b64"] = None
        return result

    def _close(self):
        if self.handle is None or self.closed:
            return
        self.closed = True
        try:
            self.io.close(self.handle)
        except Exception:
            self._set("io_cleanup")
        self._note_deadline()

    def _note_deadline(self):
        try:
            expired = self._expired()
        except IoError as exc:
            self._set(exc.code)
            return
        except Exception:
            self._set("io_failure")
            return
        if expired:
            self._set("io_deadline")

    def _ensure_time(self):
        if self._expired():
            self._set("io_deadline")
            raise _Halt

    def _expired(self):
        return self._sample() >= self.request["task_end_ticks"]

    def _sample(self):
        value = self.now()
        if type(value) is not int or isinstance(value, bool) or value < 0:
            raise IoError("io_request")
        if self.last is not None and value < self.last:
            raise IoError("io_request")
        self.last = value
        return value

    def _fail_io(self, exc):
        if isinstance(exc, FileExistsError):
            self._set("io_path")
            return
        code = getattr(exc, "io_code", None)
        self._set(code if code in IO_CODES else "io_failure")

    def _set(self, code):
        if code not in IO_CODES:
            code = "io_failure"
        if self.primary is None:
            self.primary = code
        elif code != self.primary and code not in self.cleanup:
            self.cleanup.append(code)


def _result(source, nbytes, digest, data, primary, cleanup):
    origin = source if type(source) is dict else {}

    def text(name):
        value = origin.get(name)
        return value if type(value) is str else None

    def whole(name):
        value = origin.get(name)
        return value if type(value) is int and not isinstance(value, bool) else None

    result = {
        "protocol": text("protocol"),
        "host_id": text("host_id"),
        "execution_sha256": text("execution_sha256"),
        "scripts_sha256": text("scripts_sha256"),
        "stage": whole("stage"),
        "job_id": text("job_id"),
        "operation": text("operation"),
        "bytes": nbytes,
        "sha256": digest,
        "data_b64": data,
        "primary_error": primary,
        "cleanup_errors": list(cleanup),
    }
    if set(result) != set(RESULT_FIELDS):
        raise RuntimeError("result_fields")
    encoded = json.dumps(result).encode("utf-8")
    if len(encoded) > RESULT_LIMIT:
        result["data_b64"] = None
        if result["primary_error"] is None:
            result["primary_error"] = "io_failure"
    return result
