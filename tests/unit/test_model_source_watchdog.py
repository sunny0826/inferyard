"""Independent watchdog and IPC regressions using inert children, never model endpoints."""

import json
import subprocess
import sys
import time

import pytest

from inferyard.config.preparation_io import PreparationError
from inferyard.platforms import model_source_host as host
from tests.unit.test_model_acquire_cli import SOURCE


def scripted_child(monkeypatch, script):
    original = subprocess.Popen
    children = []

    def create(args, **kwargs):
        child = original([sys.executable, "-c", script], **kwargs)
        children.append(child)
        return child

    monkeypatch.setattr(host.subprocess, "Popen", create)
    return children


def run_failure(monkeypatch, tmp_path, script, **budgets):
    children = scripted_child(monkeypatch, script)
    started = time.monotonic()
    with pytest.raises(PreparationError) as caught:
        host.acquire(SOURCE, tmp_path / "new", budget=host.Budget(**budgets))
    elapsed = time.monotonic() - started
    assert children[0].poll() is not None
    return caught.value, elapsed


FRAME_SCRIPT = """
import json, sys, time
sys.stdin.buffer.read()
def progress(stage, activity, received=0, written=0):
    print(json.dumps({'progress': {
        'stage': stage, 'activity': activity, 'received': received, 'written': written,
        'network_at': time.monotonic(),
    }}), flush=True)
"""


def test_idle_notifications_do_not_renew_read_deadline(monkeypatch, tmp_path):
    failure, elapsed = run_failure(
        monkeypatch,
        tmp_path,
        FRAME_SCRIPT + "while True:\n    progress('download-body', 'network')\n    time.sleep(.03)",
        total_seconds=2,
        read_seconds=0.25,
        cleanup_seconds=0.5,
    )
    assert elapsed < 1.2
    assert failure.diagnostics["exception_class"] == "read_timeout"
    assert failure.diagnostics["bytes_written"] == 0


def test_raw_byte_progress_survives_small_chunks_until_total_deadline(monkeypatch, tmp_path):
    failure, elapsed = run_failure(
        monkeypatch,
        tmp_path,
        FRAME_SCRIPT
        + """
for n in range(1, 100):
    progress('download-body', 'network', n, n)
    time.sleep(.07)
""",
        total_seconds=0.7,
        read_seconds=0.3,
        cleanup_seconds=0.5,
    )
    assert elapsed < 1.5
    assert failure.diagnostics["exception_class"] == "total_timeout"
    assert 3 <= failure.diagnostics["bytes_written"] < 100
    assert failure.diagnostics["average_bytes_per_second"] > 0


def test_local_publication_is_not_reported_as_idle_network(monkeypatch, tmp_path):
    failure, elapsed = run_failure(
        monkeypatch,
        tmp_path,
        FRAME_SCRIPT + "progress('publish', 'local', 8, 8)\ntime.sleep(10)",
        total_seconds=0.6,
        read_seconds=0.2,
        cleanup_seconds=0.5,
    )
    assert elapsed >= 0.6
    assert failure.diagnostics["stage"] == "publish"
    assert failure.diagnostics["exception_class"] == "total_timeout"
    assert failure.diagnostics["bytes_written"] == 8


@pytest.mark.parametrize(
    "output",
    [
        "print('SECRET' * 10000, flush=True)",
        "print('{bad SECRET}', flush=True)",
        'print(\'{"reason": "SECRET"}\', flush=True)',
        "progress('SECRET', 'network')",
    ],
)
def test_bad_worker_output_is_bounded_and_redacted(monkeypatch, tmp_path, output):
    failure, elapsed = run_failure(
        monkeypatch,
        tmp_path,
        FRAME_SCRIPT + output + "\ntime.sleep(10)",
        total_seconds=0.7,
        read_seconds=0.3,
        cleanup_seconds=0.5,
    )
    assert elapsed < 1.5
    assert "SECRET" not in str(failure) + json.dumps(failure.diagnostics)


def test_child_exit_is_classified_without_raw_stderr(monkeypatch, tmp_path):
    failure, _ = run_failure(
        monkeypatch,
        tmp_path,
        "import sys; sys.stderr.write('SECRET'); sys.exit(7)",
        total_seconds=2,
        read_seconds=1,
        cleanup_seconds=0.5,
    )
    assert failure.diagnostics["exception_class"] == "worker_exit"
    assert failure.diagnostics["bytes_written"] is None
    assert failure.diagnostics["average_bytes_per_second"] is None
    assert failure.diagnostics["progress_reason"] == "worker_progress_unavailable"


def test_cancel_after_partial_network_progress(monkeypatch, tmp_path):
    children = scripted_child(
        monkeypatch, FRAME_SCRIPT + "progress('download-body', 'network', 8, 8)\ntime.sleep(10)"
    )
    tick = host.wait_tick
    started = time.monotonic()

    def cancel_after_child_started(seconds):
        if time.monotonic() - started > 0.3:
            raise KeyboardInterrupt
        tick(seconds)

    monkeypatch.setattr(host, "wait_tick", cancel_after_child_started)
    with pytest.raises(PreparationError) as caught:
        host.acquire(SOURCE, tmp_path / "new", budget=host.Budget(cleanup_seconds=0.5))
    assert caught.value.code == 130
    assert caught.value.diagnostics["bytes_written"] == 8
    assert children[0].poll() is not None


def test_child_that_never_reads_stdin_cannot_block_parent_watchdog(monkeypatch, tmp_path):
    import threading

    children = scripted_child(monkeypatch, "import time; time.sleep(10)")
    started = time.monotonic()
    with pytest.raises(PreparationError) as caught:
        host.acquire(
            SOURCE,
            tmp_path / ("x" * (24 * 1024)),
            budget=host.Budget(total_seconds=2, read_seconds=0.25, cleanup_seconds=0.5),
        )
    assert time.monotonic() - started < 1.5
    assert caught.value.diagnostics["exception_class"] == "read_timeout"
    assert children[0].poll() is not None
    assert not any(t.name == "model-acquire-output" for t in threading.enumerate())


def test_oversized_request_is_rejected_before_child_start(monkeypatch, tmp_path):
    children = scripted_child(monkeypatch, "raise AssertionError('must not launch')")
    with pytest.raises(PreparationError, match="^invalid_model_source$"):
        host.acquire(SOURCE, tmp_path / ("x" * (33 * 1024)))
    assert not children


def test_noncontract_success_frame_is_not_forwarded(monkeypatch, tmp_path):
    failure, _ = run_failure(
        monkeypatch,
        tmp_path,
        'print(\'{"details": {"url": "SECRET"}}\', flush=True)',
        total_seconds=2,
        read_seconds=1,
        cleanup_seconds=0.5,
    )
    assert failure.diagnostics["exception_class"] == "protocol_error"
    assert "SECRET" not in json.dumps(failure.diagnostics)
