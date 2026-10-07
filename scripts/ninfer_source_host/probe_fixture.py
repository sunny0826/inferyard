"""Stage46 诊断夹具。run_fixture 在被保管的直接 worker 内执行，不再启动子进程。"""

from __future__ import annotations

import sys

from .probe_fixture_clock import ClockError, bind_request_clock
from .probe_fixture_deadline import Deadline
from .probe_fixture_frames import completion_code, loads_frame, require_request
from .probe_fixture_worker import run_worker
from .probe_io_limits import OUTPUT_PREFIX, STREAM_LIMIT

__all__ = ["main", "run_fixture"]


def run_fixture(mode: str, request: dict, *, io_api=None, native_api=None) -> int:
    """直接执行一个夹具。公共签名不变；注入时钟只能通过私有 _run_fixture。"""
    code, _frame = _run_fixture(mode, request, io_api=io_api, native_api=native_api, now_ticks=None)
    return code


def main(argv: list[str] | None = None) -> int:
    """本波没有真实入口。任何参数都返回 2，不调用 run_fixture，也不读取环境变量改根。"""
    _ = sys.argv if argv is None else argv
    return 2


def _run_fixture(mode, request, *, io_api=None, native_api=None, now_ticks=None, stdout=None):
    item = require_request(mode, request)
    clock = bind_request_clock(item, now_ticks)
    gate = Deadline(clock)
    if not gate.allow():
        raise ClockError("fixture_deadline")
    sink = sys.stdout.buffer if stdout is None else stdout
    code, accepted = run_worker(mode, item, io_api, native_api, stdout=sink, gate=gate)
    return _finish(mode, item, gate, code, accepted)


def _finish(mode, item, gate, code, accepted):
    """解析、绑定、完成码和最终决定各自采样。过 task 就否决成功，过 total 不补 I/O。"""
    if accepted is None or not accepted.flushed or gate.blocked():
        return (2 if code == 0 else code), accepted
    if mode == "output":
        if code != 0 or gate.veto or not _raw_output(accepted.payload) or not gate.work_open():
            return 2, accepted
        return 0, accepted
    parsed = loads_frame(accepted.payload)
    if not gate.work_open():
        return 2, accepted
    if not _bound(parsed, mode, item):
        raise ValueError("fixture_identity")
    if not gate.work_open():
        return 2, accepted
    final = completion_code(parsed)
    if not gate.work_open() or final != 0 or code != 0:
        return 2, accepted
    return 0, accepted


def _raw_output(raw):
    return (
        type(raw) is bytes
        and len(raw) > STREAM_LIMIT
        and raw.startswith(OUTPUT_PREFIX)
        and not raw.startswith(b"{")
    )


def _bound(frame, mode, request):
    names = ("protocol", "host_id", "execution_sha256", "scripts_sha256", "stage", "job_id")
    return frame["mode"] == mode and all(frame.get(name) == request[name] for name in names)


def _module_entry(argv):
    """模块入口始终走拒绝 main。没有环境变量旁路，也不调用 worker。"""
    return main(argv)


if __name__ == "__main__":
    raise SystemExit(_module_entry(sys.argv))
