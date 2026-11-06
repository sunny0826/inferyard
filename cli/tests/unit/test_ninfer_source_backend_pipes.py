"""Backend 管道、退出与关闭回归。继续使用注入 API，不调用真实 WinAPI。"""

import pytest

from tests.unit.test_ninfer_source_backend import (
    BackendError,
    BackendStartError,
    Pipe,
    backend,
    spec,
)


@pytest.mark.parametrize("where", ["job", "assign", "resume", "job-leak"])
def test_start_failure_keeps_original_ref(where):
    api, host = backend(where)
    with pytest.raises(BackendStartError) as caught:
        host.start(spec())
    assert caught.value.ref.process is api.process
    assert caught.value.ref.thread is api.thread
    assert api.terminated == []
    assert api.killed == []
    assert api.closed == []
    if where == "job":
        assert caught.value.ref.job is None
    elif where == "job-leak":
        assert caught.value.ref.job is None
        assert api.job in caught.value.ref.unknown
        assert "assign" not in api.order
    elif where == "assign":
        assert caught.value.ref.job is api.job
        assert "resume" not in api.order
    else:
        assert api.resumed is api.thread


def test_wrong_ref_does_not_touch_the_api():
    api, host = backend()
    host.start(spec())
    api.peeked.clear()
    foreign = object()
    with pytest.raises(BackendError):
        host.pump(foreign, 4096)
    with pytest.raises(BackendError):
        host.exit_code(foreign)
    with pytest.raises(BackendError):
        host.terminate(foreign)
    with pytest.raises(BackendError):
        host.kill(foreign)
    with pytest.raises(BackendError):
        host.close_pipes(foreign)
    with pytest.raises(BackendError):
        host.close_handle(foreign)
    assert api.peeked == []
    assert api.waited is None
    assert api.closed == []
    assert api.terminated == []
    assert api.killed == []


def test_pump_reads_only_peeked_bytes_and_caps_the_limit():
    api, host = backend()
    ref = host.start(spec())
    api.stdout = Pipe(b"abc", False)
    api.stderr = Pipe(b"z" * 5000, True)
    ref.stdout = api.stdout
    ref.stderr = api.stderr
    pumped = host.pump(ref, 100)
    assert api.read_sizes == [3, 100]
    assert pumped["stdout"] == b"abc"
    assert pumped["stderr"] == b"z" * 100
    assert pumped["eof"] is False
    assert api.closed == []


def test_pump_eof_requires_both_streams_drained():
    api, host = backend()
    ref = host.start(spec())
    api.stdout.data = b"0123456789"
    api.stdout.eof = True
    api.stderr.data = b""
    api.stderr.eof = True
    ref.stdout = api.stdout
    ref.stderr = api.stderr
    partial = host.pump(ref, 4)
    assert partial["stdout"] == b"0123"
    assert partial["eof"] is False
    drained = host.pump(ref, 4096)
    assert drained["stdout"] == b"456789"
    assert drained["stderr"] == b""
    assert drained["eof"] is True
    assert api.peeked
    assert api.closed == []


@pytest.mark.parametrize(
    ("wait_result", "wait_raises", "expect"),
    [(0x102, False, None), (0, False, 7), (0xFFFFFFFF, False, "error"), (0, True, "error")],
)
def test_exit_code_uses_original_handle_and_wait_failure_is_not_exit(
    wait_result, wait_raises, expect
):
    api, host = backend()
    ref = host.start(spec())
    api.wait_result = wait_result
    api.wait_raises = wait_raises
    api.exit_value = 7
    if expect == "error":
        with pytest.raises(BackendError):
            host.exit_code(ref)
        assert api.exit_process is None
    else:
        assert host.exit_code(ref) == expect
        assert api.waited is api.process
        if expect is None:
            assert api.exit_process is None
        else:
            assert api.exit_process is api.process


def test_bool_and_unknown_exit_payload_are_not_exits():
    api, host = backend()
    ref = host.start(spec())
    api.wait_result = False
    with pytest.raises(BackendError):
        host.exit_code(ref)
    assert api.exit_process is None
    api.wait_result = 0
    api.exit_value = None
    with pytest.raises(BackendError):
        host.exit_code(ref)
    api.exit_raises = True
    api.exit_value = 0
    with pytest.raises(BackendError):
        host.exit_code(ref)
    assert api.exit_process is api.process


def test_close_failure_does_not_drop_or_retry_the_ref():
    api, host = backend()
    ref = host.start(spec())
    api.close_raises = True
    with pytest.raises(BackendError):
        host.close_handle(ref)
    assert api.closed == [api.process]
    assert ref.process is api.process
    assert ref.job is api.job
    with pytest.raises(BackendError):
        host.close_handle(ref)
    assert api.closed == [api.process]
    assert api.terminated == []


def test_close_pipes_is_separate_from_the_process_handle():
    api, host = backend()
    ref = host.start(spec())
    host.close_pipes(ref)
    assert api.closed == [api.stdout, api.stderr]
    with pytest.raises(BackendError):
        host.close_pipes(ref)
    assert api.closed == [api.stdout, api.stderr]
    host.terminate(ref)
    host.kill(ref)
    assert api.terminated == [api.process]
    assert api.killed == [api.process]
    assert api.process not in api.closed
