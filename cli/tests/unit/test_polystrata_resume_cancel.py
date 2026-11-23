"""Unit tests for PolyStrata setup resume and process-group cancellation."""

from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

import pytest

from inferyard.provisioning import polystrata_prepare
from inferyard.provisioning.polystrata_preflight import GIB
from inferyard.provisioning.preparation_io import PreparationError
from polystrata_test_helpers import (
    _HAPPY_SETUP_PY,
    _FakeClient,
    _FakeResponse,
    _disk_usage,
    _make_fake_venv,
    _tar_gz_file,
)


def test_resume_after_setup_failure_and_remote_range_resume(tmp_path: Path):
    # Case A: fail on first setup.py invocation leaving partial data, succeed on --resume
    flaky_setup = """\
import json, os, sys
from pathlib import Path

root = Path(__file__).resolve().parent
data_dir = Path(sys.argv[sys.argv.index("--data-dir") + 1])
part = data_dir / "model.gguf.part"
if not part.exists():
    part.write_bytes(b"partial-weights")
    print("=== Step 5: downloading model ===", flush=True)
    sys.exit(1)

assert part.read_bytes() == b"partial-weights"
engine_dir = root / "engine-poly"
engine_dir.mkdir(exist_ok=True)
binary = engine_dir / ("strata-poly.exe" if os.name == "nt" else "strata-poly")
binary.write_text("#!/bin/sh\\n", encoding="utf-8")
if os.name != "nt":
    os.chmod(binary, 0o755)
(root / "strata-resumed.json").write_text(
    json.dumps(
        {
            "exe": str(binary),
            "args": ["--model", str(part)],
            "cwd": str(root),
            "model_name": "m",
            "port": 8080,
        }
    ),
    encoding="utf-8",
)
"""
    archive = _tar_gz_file(
        tmp_path / "flaky.tar.gz",
        [
            ("file", "setup.py", flaky_setup.encode("utf-8")),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
        ],
    )
    out = tmp_path / "ps-resume"
    data = tmp_path / "data-resume"

    with pytest.raises(PreparationError) as exc_info:
        polystrata_prepare.prepare_polystrata(
            out,
            data_dir=data,
            source=str(archive),
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            venv_creator=_make_fake_venv,
        )
    assert exc_info.value.reason == "polystrata_setup_failed"
    assert (data / "model.gguf.part").read_bytes() == b"partial-weights"

    # Without --resume, fails closed with output_exists
    with pytest.raises(PreparationError) as exc_exists:
        polystrata_prepare.prepare_polystrata(
            out,
            data_dir=data,
            source=str(archive),
            resume=False,
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            venv_creator=_make_fake_venv,
        )
    assert exc_exists.value.reason == "output_exists"

    # With --resume, reuses extracted runtime & partial data and succeeds
    resumed = polystrata_prepare.prepare_polystrata(
        out,
        data_dir=data,
        source=str(archive),
        resume=True,
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(200 * GIB),
        venv_creator=_make_fake_venv,
    )
    assert resumed["resumed"] is True
    assert Path(resumed["installation_path"]).is_file()

    # Case B: remote HTTPS archive resume via Range 206
    remote_archive = _tar_gz_file(
        tmp_path / "remote.tar.gz",
        [
            ("file", "setup.py", _HAPPY_SETUP_PY.encode("utf-8")),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
        ],
    )
    payload = remote_archive.read_bytes()
    digest = hashlib.sha256(payload).hexdigest()
    split = len(payload) // 2
    out_remote = tmp_path / "ps-remote"
    out_remote.mkdir()
    (out_remote / "v0.1.45-poly.10.tar.gz.part").write_bytes(payload[:split])

    url = "https://github.com/simplexai-labs/PolyStrata/archive/refs/tags/v0.1.45-poly.10.tar.gz"

    def _handler(_method, _url, headers):
        assert headers.get("Range") == f"bytes={split}-"
        return _FakeResponse(
            payload[split:],
            status_code=206,
            headers={
                "Content-Range": f"bytes {split}-{len(payload) - 1}/{len(payload)}",
                "Content-Length": str(len(payload) - split),
            },
        )

    client = _FakeClient(_handler)
    remote_res = polystrata_prepare.prepare_polystrata(
        out_remote,
        data_dir=tmp_path / "data-remote",
        source=url,
        expected_sha256=digest,
        resume=True,
        client=client,
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(200 * GIB),
        venv_creator=_make_fake_venv,
    )
    assert remote_res["ready_to_run"] is True
    assert len(client.requests) == 1


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
    # Pre-seed extracted tree with valid runtime receipt & lightweight venv so setup.py runs immediately
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

    # Run via python -c invoking prepare_polystrata with mocked preflight so the test
    # does not depend on host disk free space or Xcode CLT in CI containers.
    driver = f"""\
from pathlib import Path
from types import SimpleNamespace
import sys
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

        # Confirm grandchild was terminated along with setup.py's process group
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
