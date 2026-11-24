"""Unit tests for PolyStrata setup process-group cancellation."""

from __future__ import annotations

import hashlib
import json
import os
import signal
import subprocess
import sys
import time
from pathlib import Path

import pytest

from polystrata_test_helpers import _make_fake_venv, _tar_gz_file


@pytest.mark.skipif(os.name == "nt", reason="POSIX SIGTERM process-group cancellation test")
def test_cancellation_cleans_up_process_tree_and_preserves_partial_downloads(tmp_path: Path):
    """SIGTERM must kill even a grandchild that ignores SIGTERM after the leader exits."""
    child_pid_file = tmp_path / "grandchild.pid"
    slow_setup = f"""\
import signal, subprocess, sys, time
from pathlib import Path

data_dir = Path(sys.argv[sys.argv.index("--data-dir") + 1])
(data_dir / "partial.gguf.part").write_bytes(b"keep-for-resume")
child = subprocess.Popen(
    [
        sys.executable,
        "-c",
        "import signal, time; signal.signal(signal.SIGTERM, signal.SIG_IGN); time.sleep(60)",
    ]
)
Path({str(child_pid_file)!r}).write_text(str(child.pid), encoding="utf-8")
signal.signal(signal.SIGTERM, lambda _s, _f: sys.exit(0))
print("=== Step 5: downloading model ===", flush=True)
print("READY_FOR_CANCEL", flush=True)
time.sleep(60)
"""
    archive = _tar_gz_file(
        tmp_path / "slow.tar.gz",
        [
            ("file", "setup.py", slow_setup.encode("utf-8")),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
        ],
    )
    archive_sha = hashlib.sha256(archive.read_bytes()).hexdigest()
    out = tmp_path / "ps-cancel"
    data = tmp_path / "data-cancel"
    out.mkdir()
    (out / "setup.py").write_text(slow_setup, encoding="utf-8")
    (out / "serve").mkdir()
    (out / "serve" / "server.py").write_text("# server\n", encoding="utf-8")
    (out / ".inferyard-runtime.json").write_text(
        json.dumps(
            {
                "definition": "polystrata-runtime.v1",
                "source": f"local:{archive.resolve()}",
                "archive_sha256": archive_sha,
            }
        ),
        encoding="utf-8",
    )
    _make_fake_venv(out)

    driver = f"""\
from pathlib import Path
from types import SimpleNamespace
import sys
from inferyard.platforms.local_engines.detect_polystrata import pick_venv_python
from inferyard.provisioning import polystrata_prepare
from inferyard.provisioning.preparation_io import PreparationError

try:
    polystrata_prepare.prepare_polystrata(
        Path({str(out)!r}),
        data_dir=Path({str(data)!r}),
        source={str(archive)!r},
        resume=True,
        progress_stream=True,
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=lambda _p: SimpleNamespace(total=400*1024**3, used=0, free=200*1024**3),
        venv_creator=lambda p: pick_venv_python(p / ".venv", platform_system="Darwin"),
    )
except PreparationError as exc:
    sys.exit(exc.code)
"""
    proc = subprocess.Popen(
        [sys.executable, "-c", driver],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    try:
        deadline = time.monotonic() + 10.0
        while time.monotonic() < deadline and not child_pid_file.exists():
            time.sleep(0.05)
        assert child_pid_file.exists(), f"grandchild never started; stderr={proc.stderr.read()}"
        grandchild_pid = int(child_pid_file.read_text(encoding="utf-8").strip())

        proc.send_signal(signal.SIGTERM)
        rc = proc.wait(timeout=10.0)
        assert rc == 130

        reap_deadline = time.monotonic() + 3.0
        alive = True
        while time.monotonic() < reap_deadline:
            try:
                os.kill(grandchild_pid, 0)
            except ProcessLookupError:
                alive = False
                break
            time.sleep(0.05)
        assert not alive, f"grandchild pid {grandchild_pid} survived SIGTERM"
        assert (data / "partial.gguf.part").read_bytes() == b"keep-for-resume"
    finally:
        if proc.poll() is None:
            proc.kill()
            proc.wait(timeout=5)
