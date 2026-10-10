"""Real direct-child timeout/cancellation against local, inert subprocess fixtures."""

import json
import subprocess
import sys
import time

import pytest

from inferyard.cli import main
from inferyard.config.preparation_io import PreparationError
from inferyard.platforms import model_source_host as host
from tests.unit.test_model_acquire_cli import SOURCE


def hanging_child(monkeypatch, tmp_path, *, cancel=False, ignore_terminate=False):
    popen = subprocess.Popen
    children = []
    out = tmp_path / "new"
    script = """
import pathlib, signal, sys, time
if sys.argv[2] == 'ignore' and hasattr(signal, 'SIGTERM'):
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
out = pathlib.Path(sys.argv[1])
out.mkdir()
(out / 'file.gguf.part').write_bytes(b'partial synthetic data')
while True:
    time.sleep(1)
"""

    def create(args, **kwargs):
        assert args == [sys.executable, "-m", "inferyard.platforms.model_source_worker"]
        process = popen(
            [sys.executable, "-c", script, str(out), "ignore" if ignore_terminate else "normal"],
            **kwargs,
        )
        children.append(process)
        if cancel:

            def communicate(*a, **k):
                deadline = time.monotonic() + 3
                while not (out / "file.gguf.part").exists():
                    assert time.monotonic() < deadline
                    time.sleep(0.01)
                raise KeyboardInterrupt

            process.communicate = communicate
        return process

    monkeypatch.setattr(host.subprocess, "Popen", create)
    return out, children


def test_total_deadline_kills_hung_io_child_and_preserves_failed_directory(tmp_path, monkeypatch):
    out, children = hanging_child(monkeypatch, tmp_path, ignore_terminate=True)
    started = time.monotonic()
    with pytest.raises(PreparationError, match="^model_acquire_incomplete$"):
        host.acquire(SOURCE, out, budget=host.Budget(total_seconds=0.4, cleanup_seconds=1))
    assert time.monotonic() - started < 2
    assert children[0].poll() is not None
    assert (out / "file.gguf.part").read_bytes() == b"partial synthetic data"
    assert not (out / "source.json").exists()


def test_cancellation_stops_child_and_maps_to_130(tmp_path, monkeypatch, capsys):
    out, children = hanging_child(monkeypatch, tmp_path, cancel=True)
    assert main(["model", "acquire", "--source", SOURCE, "--out", str(out)]) == 130
    captured = capsys.readouterr()
    assert json.loads(captured.out)["limitations"] == ["cancelled"]
    assert children[0].poll() is not None
    assert out.is_dir()


def test_real_worker_rejects_existing_output_without_network(tmp_path, capsys):
    out = tmp_path / "exists"
    out.mkdir()
    assert main(["model", "acquire", "--source", SOURCE, "--out", str(out)]) == 2
    assert json.loads(capsys.readouterr().out)["limitations"] == ["output_exists"]


def test_real_worker_rejects_unset_token_without_network(tmp_path, monkeypatch, capsys):
    monkeypatch.delenv("INFERYARD_TEST_UNSET_TOKEN", raising=False)
    out = tmp_path / "new"
    assert (
        main(
            [
                "model",
                "acquire",
                "--source",
                SOURCE,
                "--out",
                str(out),
                "--token-env",
                "INFERYARD_TEST_UNSET_TOKEN",
            ]
        )
        == 2
    )
    assert json.loads(capsys.readouterr().out)["limitations"] == ["model_acquire_incomplete"]
    assert not out.exists()


@pytest.mark.parametrize(
    "fields",
    [
        dict(max_bytes=True),
        dict(max_bytes=21 * 1024**3),
        dict(total_seconds=float("nan")),
        dict(read_seconds=181),
        dict(cleanup_seconds=61),
    ],
)
def test_budget_cannot_expand_general_limits(fields):
    with pytest.raises(PreparationError, match="invalid_model_source"):
        host.Budget(**fields)


def test_failed_stop_does_not_restart_cleanup_deadline(tmp_path, monkeypatch):
    class Unreapable:
        returncode = None
        stdin = None
        stdout = None
        stops = 0

        def communicate(self, *a, **k):
            raise subprocess.TimeoutExpired("fixture", 0)

        def terminate(self):
            self.stops += 1

        def wait(self, *a, **k):
            raise subprocess.TimeoutExpired("fixture", 0)

        def kill(self):
            pass

        def poll(self):
            return None

    process = Unreapable()
    monkeypatch.setattr(host.subprocess, "Popen", lambda *a, **k: process)
    with pytest.raises(PreparationError, match="io_error"):
        host.acquire(SOURCE, tmp_path / "new", budget=host.Budget(cleanup_seconds=0.01))
    assert process.stops == 1
