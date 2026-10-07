"""Backend 管道、退出与关闭回归。继续使用注入 API，不调用真实 WinAPI。"""

import ast

import pytest

from scripts.ninfer_source_host.custody import BackendError as RealBackendError
from scripts.ninfer_source_host.custody import BackendStartError as RealStartError
from tests.unit.test_ninfer_source_backend import (
    BackendError,
    BackendStartError,
    FakeApi,
    HostError,
    Pipe,
    WindowsBackend,
    backend,
    spec,
)

ROOT = __import__("pathlib").Path(__file__).resolve().parents[2]


def test_spawn_failure_before_process_has_null_ref():
    api, host = backend("spawn")
    with pytest.raises(BackendStartError) as caught:
        host.start(spec())
    assert type(caught.value) is BackendStartError
    assert caught.value.ref is None
    assert isinstance(caught.value.cause, OSError)
    assert api.order == ["spawn"]
    assert api.terminated == []
    assert api.closed == []


def test_partial_spawn_keeps_same_objects():
    api, host = backend("partial")
    with pytest.raises(BackendStartError) as caught:
        host.start(spec())
    assert caught.value.ref.process is api.process
    assert caught.value.ref.stdout is api.stdout
    assert len(caught.value.ref.extra) == 1
    assert "resume" not in api.order
    assert api.killed == []


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
        assert api.order == ["spawn", "job"]
        assert caught.value.ref.job is None
    elif where == "job-leak":
        assert caught.value.ref.job is None
        assert api.job in caught.value.ref.unknown
        assert "assign" not in api.order
    elif where == "assign":
        assert api.order == ["spawn", "job", "assign"]
        assert caught.value.ref.job is api.job
        assert "resume" not in api.order
    else:
        assert api.order == ["spawn", "job", "assign", "resume"]
        assert api.resumed is api.thread


def test_real_start_error_matches_custody_shape():
    api = FakeApi("assign")
    host = WindowsBackend(api)
    with pytest.raises(RealStartError) as caught:
        host.start(spec())
    assert type(caught.value) is RealStartError
    assert caught.value.ref.process is api.process
    assert issubclass(RealStartError, RealBackendError)


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


@pytest.mark.parametrize("limit", [0, 4097, True, 1.5])
def test_pump_rejects_bad_limit_without_reading(limit):
    api, host = backend()
    ref = host.start(spec())
    api.peeked.clear()
    with pytest.raises(HostError) as caught:
        host.pump(ref, limit)
    assert caught.value.code == "host_output_limit"
    assert api.peeked == []
    assert api.read_sizes == []


def test_read_past_peek_is_backend_error():
    api, host = backend()
    ref = host.start(spec())
    api.stdout.data = b"ab"
    api.stdout.eof = False
    api.stderr.eof = True
    ref.stdout = api.stdout
    ref.stderr = api.stderr

    def oversized(_pipe, size):
        return b"x" * (size + 1)

    api.read = oversized
    with pytest.raises(BackendError):
        host.pump(ref, 2)


def test_short_read_is_not_eof():
    api, host = backend()
    ref = host.start(spec())
    api.stdout.data = b"abcd"
    api.stdout.eof = True
    api.stderr.eof = True
    ref.stdout, ref.stderr = api.stdout, api.stderr

    def short(_pipe, _size):
        return b"a"

    api.read = short
    with pytest.raises(BackendError):
        host.pump(ref, 4)


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


def test_close_handle_runs_once_and_failure_is_not_retried():
    api, host = backend()
    ref = host.start(spec())
    host.close_handle(ref)
    assert api.closed == [api.process, api.thread, api.job]
    with pytest.raises(BackendError):
        host.close_handle(ref)
    assert api.closed == [api.process, api.thread, api.job]


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


def test_backend_sources_do_not_rebuild_a_pid_or_open_files():
    folder = ROOT / "scripts" / "ninfer_source_host"
    for name in ("windows_backend.py", "windows_backend_winapi.py", "windows_backend_limits.py"):
        source = (folder / name).read_text()
        tree = ast.parse(source)
        imported = []
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                imported.extend(alias.name.split(".")[0] for alias in node.names)
            elif isinstance(node, ast.ImportFrom) and node.module:
                imported.append(node.module.split(".")[0])
        assert {"subprocess", "pathlib", "os"}.isdisjoint(imported)
        for banned in ("OpenProcess", "EnumProcesses", "CreateToolhelp32Snapshot", "pid_exists"):
            assert banned not in source
        assert "open(" not in source
    backend_tree = ast.parse((folder / "windows_backend.py").read_text())
    for node in backend_tree.body:
        if isinstance(node, ast.ImportFrom):
            assert node.module != "custody"
