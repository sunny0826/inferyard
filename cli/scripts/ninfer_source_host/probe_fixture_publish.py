"""把诊断字节写到调用方传入的 stdout。短写、写失败和 flush 失败都不是成功。"""

from __future__ import annotations


class PublishError(OSError):
    def __init__(self, reason, written=0):
        super().__init__(reason)
        self.reason = reason
        self.written = written


def write_exact(stdout, payload):
    if type(payload) is not bytes:
        raise PublishError("publish_type")
    try:
        written = stdout.write(payload)
    except PublishError:
        raise
    except Exception as exc:
        raise PublishError("publish_write") from exc
    if type(written) is not int or isinstance(written, bool) or written != len(payload):
        accepted = written if type(written) is int and not isinstance(written, bool) else 0
        raise PublishError("publish_short", accepted)
    return written


def flush_exact(stdout):
    try:
        stdout.flush()
    except Exception as exc:
        raise PublishError("publish_flush") from exc


class Accepted:
    """write 计数和 flush 是否返回。成功不回读 stdout。"""

    def __init__(self, payload, written, flushed):
        self.payload = payload
        self.written = written
        self.flushed = flushed


def publish_bytes(stdout, gate, payload, *, cleanup=False):
    """发布开始前重新采样。成功写入只在 task 内；失败记录可到原 total。"""
    if gate is not None and not (gate.cleanup_open() if cleanup else gate.work_open()):
        return Accepted(payload, 0, False)
    try:
        written = write_exact(stdout, payload)
    except PublishError as exc:
        return Accepted(payload, exc.written, False)
    if gate is not None and not (gate.cleanup_open() if cleanup else gate.work_open()):
        return Accepted(payload, written, False)
    try:
        flush_exact(stdout)
    except PublishError:
        return Accepted(payload, written, False)
    if gate is not None:
        gate.after()
    return Accepted(payload, written, True)
