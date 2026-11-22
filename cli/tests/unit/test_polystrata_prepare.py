"""Unit and integration tests for managed `inferyard engine prepare-polystrata`."""

from __future__ import annotations

from contextlib import contextmanager
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import signal
import stat
import subprocess
import sys
import tarfile
import time
from types import SimpleNamespace
import zipfile

import httpx
import pytest

from inferyard.application.polystrata_prepare import execute as execute_polystrata_prepare
from inferyard.cli.arguments import parser
from inferyard.cli.request import build_request
from inferyard.platforms.local_engines.detect import detect_local_engines
from inferyard.platforms.local_engines.installation import read_installation
from inferyard.provisioning import polystrata_prepare
from inferyard.provisioning.polystrata_preflight import GIB, run_polystrata_preflight, sanitize_log_line
from inferyard.provisioning.preparation_io import PreparationError
from inferyard.provisioning.transfer_progress import ProgressEmitter


def _disk_usage(free_bytes: int):
    return lambda _path: SimpleNamespace(total=free_bytes * 2, used=free_bytes, free=free_bytes)


def _tar_gz_file(path: Path, entries: list[tuple]) -> Path:
    buf = io.BytesIO()
    with gzip.GzipFile(fileobj=buf, mode="wb", mtime=0) as gz:
        with tarfile.open(fileobj=gz, mode="w") as package:
            for entry in entries:
                kind, name, *rest = entry
                info = tarfile.TarInfo(name)
                if kind == "dir":
                    info.type = tarfile.DIRTYPE
                    info.mode = rest[0] if rest else 0o755
                    package.addfile(info)
                elif kind == "symlink":
                    info.type = tarfile.SYMTYPE
                    info.linkname = rest[0]
                    package.addfile(info)
                else:
                    data = rest[0]
                    info.size = len(data)
                    info.mode = rest[1] if len(rest) > 1 else 0o644
                    package.addfile(info, io.BytesIO(data))
    path.write_bytes(buf.getvalue())
    return path


def _zip_file(path: Path, entries: dict[str, bytes]) -> Path:
    with zipfile.ZipFile(path, "w") as package:
        for name, data in entries.items():
            package.writestr(name, data)
    return path


def _make_fake_venv(out: Path) -> Path:
    """Fast lightweight venv wrapper pointing at current test Python (no ensurepip delay)."""
    venv = out / ".venv"
    bin_dir = venv / ("Scripts" if os.name == "nt" else "bin")
    bin_dir.mkdir(parents=True, exist_ok=True)
    (venv / "pyvenv.cfg").write_text("home = /usr/bin\ninclude-system-site-packages = false\n")
    py = bin_dir / ("python.exe" if os.name == "nt" else "python")
    if os.name == "nt":
        py.write_text(f"@echo off\n\"{sys.executable}\" %*\n", encoding="utf-8")
    else:
        py.write_text(f"#!/bin/sh\nexec \"{sys.executable}\" \"$@\"\n", encoding="utf-8")
        os.chmod(py, 0o755)
    return py


class _FakeResponse:
    def __init__(self, body: bytes, *, status_code: int = 200, headers: dict | None = None):
        self._body = body
        self.status_code = status_code
        self.headers = httpx.Headers(headers or {"Content-Length": str(len(body))})

    def iter_raw(self):
        mid = len(self._body) // 2
        if mid:
            yield self._body[:mid]
        yield self._body[mid:]


class _FakeClient:
    def __init__(self, handler):
        self._handler = handler
        self.requests: list[tuple[str, str, dict]] = []

    @contextmanager
    def stream(self, method: str, url: str, *, headers: dict | None = None):
        req_headers = dict(headers or {})
        self.requests.append((method, url, req_headers))
        yield self._handler(method, url, req_headers)


_HAPPY_SETUP_PY = """\
import json, os, stat, sys
from pathlib import Path

root = Path(__file__).resolve().parent
argv = sys.argv[1:]
(root / "received_argv.json").write_text(json.dumps(argv), encoding="utf-8")
(root / "received_env.json").write_text(
    json.dumps({k: os.environ.get(k) for k in ("HTTP_PROXY", "HTTPS_PROXY", "STRATA_SOURCE")}),
    encoding="utf-8",
)

print("=== Step 3: Python packages ===", flush=True)
print("Authorization: Bearer secret_token_1234567890 hf_abcdefghijklmnop", flush=True)
print("=== Step 4: the PolyStrata engine ===", flush=True)
print("[ 50%] Building CXX object strata-poly.o", flush=True)
print("[100%] Linking CXX executable strata-poly", flush=True)
print("=== Step 5: downloading Qwen3.6-35B-A3B UD-Q4_K_M ===", flush=True)
sys.stdout.write("\\r  Qwen3.6.gguf: 1.50 / 3.00 GB (50%)   ")
sys.stdout.flush()
sys.stdout.write("\\r  Qwen3.6.gguf: 3.00 / 3.00 GB (100%)   \\n")
sys.stdout.flush()
print("=== Step 7: writing the start script ===", flush=True)

engine_dir = root / "engine-poly"
engine_dir.mkdir(exist_ok=True)
binary = engine_dir / ("strata-poly.exe" if os.name == "nt" else "strata-poly")
binary.write_text("#!/bin/sh\\necho strata-poly\\n", encoding="utf-8")
if os.name != "nt":
    os.chmod(binary, 0o755)
(engine_dir / "BUILD.json").write_text(
    json.dumps({"version": "v0.1.45-poly.10", "backend": "metal"}), encoding="utf-8"
)
(root / "strata-qwen36.json").write_text(
    json.dumps(
        {
            "exe": str(binary),
            "args": ["--model", "model.gguf", "--max-context", "32768"],
            "cwd": str(root),
            "model_name": "qwen36-ud-q4-k-m",
            "port": 8080,
        }
    ),
    encoding="utf-8",
)
"""


def test_progress_emitter_backward_compat_and_optional_fields():
    buf = io.StringIO()
    emitter = ProgressEmitter(enabled=True, stream=buf, min_interval_seconds=0.0)
    emitter.update("downloading", 500, 1000)
    emitter.update(
        "setup_engine",
        0,
        None,
        step=4,
        step_total=7,
        percent=62.5,
        detail="  building\nstrata-poly  ",
    )
    lines = [json.loads(line) for line in buf.getvalue().splitlines()]
    assert set(lines[0]) == {
        "event",
        "stage",
        "downloaded_bytes",
        "total_bytes",
        "bytes_per_second",
    }
    assert lines[1]["stage"] == "setup_engine"
    assert lines[1]["step"] == 4
    assert lines[1]["step_total"] == 7
    assert lines[1]["percent"] == 62.5
    assert lines[1]["detail"] == "building strata-poly"


def test_sanitize_log_line_redacts_secrets_and_ansi():
    raw = "\x1b[32mOK\x1b[0m https://alice:s3cret@host/x Bearer tok123456 hf_1234567890ab --api-key mykey"
    cleaned = sanitize_log_line(raw)
    assert "\x1b" not in cleaned
    assert "s3cret" not in cleaned
    assert "tok123456" not in cleaned
    assert "hf_1234567890ab" not in cleaned
    assert "mykey" not in cleaned
    assert "[REDACTED]" in cleaned


def test_preflight_blockers_are_read_only(tmp_path: Path):
    out = tmp_path / "out-dir"
    data = tmp_path / "data-dir"

    # Unsupported platform
    with pytest.raises(PreparationError) as exc_info:
        run_polystrata_preflight(
            out, data, platform_info=("Linux", "arm64"), disk_usage_fn=_disk_usage(200 * GIB)
        )
    assert exc_info.value.reason == "polystrata_platform_unsupported"
    assert not out.exists() and not data.exists()

    # macOS < 15 and missing Xcode CLT
    with pytest.raises(PreparationError) as exc_info:
        run_polystrata_preflight(
            out,
            data,
            platform_info=("Darwin", "arm64"),
            macos_version="14.7.1",
            xcode_clt_check=lambda: False,
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    codes = [b["code"] for b in exc_info.value.details["blockers"]]
    assert "polystrata_macos_too_old" in codes
    assert "polystrata_xcode_clt_missing" in codes
    clt_blocker = next(
        b for b in exc_info.value.details["blockers"] if b["code"] == "polystrata_xcode_clt_missing"
    )
    assert clt_blocker["fix_command"] == "xcode-select --install"

    # Linux missing python3-venv + old NVIDIA driver
    with pytest.raises(PreparationError) as exc_info:
        run_polystrata_preflight(
            out,
            data,
            platform_info=("Linux", "x64"),
            linux_venv_check=lambda _py: False,
            gpu_probe=lambda _sys: {
                "status": "available",
                "devices": [{"vendor": "nvidia", "driver_version": "550.54.14"}],
            },
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    codes = [b["code"] for b in exc_info.value.details["blockers"]]
    assert "polystrata_python_venv_missing" in codes
    assert "polystrata_nvidia_driver_too_old" in codes
    venv_blocker = next(
        b for b in exc_info.value.details["blockers"] if b["code"] == "polystrata_python_venv_missing"
    )
    assert "python3-venv" in venv_blocker["fix_command"]

    # Insufficient disk space
    with pytest.raises(PreparationError) as exc_info:
        run_polystrata_preflight(
            out,
            data,
            family="qwen36",
            model="UD-Q4_K_M",
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(5 * GIB),
        )
    assert exc_info.value.reason == "polystrata_disk_insufficient"
    assert not out.exists() and not data.exists()

    # Backend mismatch
    with pytest.raises(PreparationError) as exc_info:
        run_polystrata_preflight(
            out,
            data,
            backend="cuda",
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    assert exc_info.value.reason == "polystrata_backend_unsupported"

    # Missing GGUF path
    with pytest.raises(PreparationError) as exc_info:
        run_polystrata_preflight(
            out,
            data,
            gguf_path=tmp_path / "missing.gguf",
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    assert exc_info.value.reason == "polystrata_gguf_path_missing"


def test_happy_path_local_tar_progress_log_sanitization_and_installation(tmp_path: Path):
    archive = _tar_gz_file(
        tmp_path / "PolyStrata-v0.1.45-poly.10.tar.gz",
        [
            ("dir", "PolyStrata-0.1.45-poly.10"),
            ("file", "PolyStrata-0.1.45-poly.10/setup.py", _HAPPY_SETUP_PY.encode("utf-8")),
            ("dir", "PolyStrata-0.1.45-poly.10/serve"),
            ("file", "PolyStrata-0.1.45-poly.10/serve/server.py", b"# fake server\n"),
        ],
    )
    out = tmp_path / "polystrata-install"
    data = tmp_path / "polystrata-data"
    progress_buf = io.StringIO()
    emitter = ProgressEmitter(enabled=True, stream=progress_buf, min_interval_seconds=0.0)

    details = polystrata_prepare.prepare_polystrata(
        out,
        data_dir=data,
        family="qwen36",
        model="UD-Q4_K_M",
        context=32768,
        model_source="modelscope",
        source=str(archive),
        proxy_origin="http://127.0.0.1:7890",
        emitter=emitter,
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(200 * GIB),
        venv_creator=_make_fake_venv,
    )

    assert details["ready_to_run"] is True
    assert details["version"] == "v0.1.45-poly.10"
    assert details["backend"] == "metal"
    assert Path(details["installation_path"]).is_file()
    assert Path(details["model_config_path"]).name == "strata-qwen36.json"
    assert details["resumed"] is False

    # Verify installation.json validates cleanly via read_installation and detect_local_engines
    loaded = read_installation(details["installation_path"])
    assert loaded["family"] == "polystrata"
    assert loaded["working_directory"] == str(out.resolve())
    assert loaded["data_dir"] == str(data.resolve())
    detected = detect_local_engines(extra_paths=[out])
    assert any(
        c["family"] == "polystrata" and c["working_directory"] == str(out.resolve())
        for c in detected["candidates"]
    )

    # Verify setup.py received expected flags and proxy env
    received_argv = json.loads((out / "received_argv.json").read_text(encoding="utf-8"))
    assert "--yes" in received_argv
    assert "--no-start" in received_argv
    assert ["--family", "qwen36"] == received_argv[received_argv.index("--family") : received_argv.index("--family") + 2]
    assert ["--model", "UD-Q4_K_M"] == received_argv[received_argv.index("--model") : received_argv.index("--model") + 2]
    assert ["--context", "32768"] == received_argv[received_argv.index("--context") : received_argv.index("--context") + 2]
    assert ["--source", "modelscope"] == received_argv[received_argv.index("--source") : received_argv.index("--source") + 2]
    received_env = json.loads((out / "received_env.json").read_text(encoding="utf-8"))
    assert received_env["HTTPS_PROXY"] == "http://127.0.0.1:7890"
    assert received_env["STRATA_SOURCE"] == "modelscope"

    # Verify NDJSON progress stages & download percentages
    events = [json.loads(line) for line in progress_buf.getvalue().splitlines()]
    stages = [ev["stage"] for ev in events]
    assert "preflight" in stages
    assert "downloading_runtime" in stages
    assert "setup_engine" in stages
    assert "downloading_model" in stages
    assert stages[-1] == "verifying"
    assert events[-1]["percent"] == 100.0
    dl_events = [ev for ev in events if ev["stage"] == "downloading_model" and ev.get("percent") == 50.0]
    assert dl_events and dl_events[0]["downloaded_bytes"] == 1_500_000_000

    # Verify log file sanitized secrets
    log_text = Path(details["log_path"]).read_text(encoding="utf-8")
    assert "secret_token_1234567890" not in log_text
    assert "hf_abcdefghijklmnop" not in log_text
    assert "[REDACTED]" in log_text


def test_gguf_path_passthrough_linux_and_macos(tmp_path: Path):
    archive = _zip_file(
        tmp_path / "polystrata.zip",
        {
            "PolyStrata-0.1.45-poly.10/setup.py": _HAPPY_SETUP_PY.encode("utf-8"),
            "PolyStrata-0.1.45-poly.10/serve/server.py": b"# server\n",
        },
    )
    gguf_dir = tmp_path / "shared-gguf"
    gguf_dir.mkdir()
    gguf_file = gguf_dir / "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf"
    gguf_file.write_bytes(b"GGUF\x03\x00\x00\x00")

    # Linux -> --custom-gguf <file>
    out_linux = tmp_path / "ps-linux"
    polystrata_prepare.prepare_polystrata(
        out_linux,
        data_dir=tmp_path / "data-linux",
        gguf_path=gguf_file,
        backend="cuda",
        source=str(archive),
        platform_info=("Linux", "x64"),
        linux_venv_check=lambda _py: True,
        gpu_probe=lambda _sys: {
            "status": "available",
            "devices": [{"vendor": "nvidia", "driver_version": "580.65.06"}],
        },
        disk_usage_fn=_disk_usage(4 * GIB),  # Enough when reusing GGUF!
        venv_creator=_make_fake_venv,
    )
    argv_linux = json.loads((out_linux / "received_argv.json").read_text(encoding="utf-8"))
    idx = argv_linux.index("--custom-gguf")
    assert argv_linux[idx + 1] == str(gguf_file.resolve())
    assert "--backend" in argv_linux and argv_linux[argv_linux.index("--backend") + 1] == "cuda"

    # macOS -> --gguf-dir <parent_dir> (matches upstream metal/setup_mac.py)
    out_mac = tmp_path / "ps-mac"
    polystrata_prepare.prepare_polystrata(
        out_mac,
        data_dir=tmp_path / "data-mac",
        gguf_path=gguf_file,
        backend="metal",
        source=str(archive),
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(4 * GIB),
        venv_creator=_make_fake_venv,
    )
    argv_mac = json.loads((out_mac / "received_argv.json").read_text(encoding="utf-8"))
    idx_mac = argv_mac.index("--gguf-dir")
    assert argv_mac[idx_mac + 1] == str(gguf_dir.resolve())
    assert "--backend" not in argv_mac


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
    json.dumps({"exe": str(binary), "args": [], "cwd": str(root), "model_name": "m", "port": 8080}),
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


def test_archive_rejects_unsafe_entries_and_invalid_sources(tmp_path: Path):
    evil_tar = _tar_gz_file(
        tmp_path / "evil.tar.gz",
        [
            ("file", "setup.py", b"print(1)\n"),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
            ("symlink", "escape", "../../etc/passwd"),
        ],
    )
    with pytest.raises(PreparationError) as exc_info:
        polystrata_prepare.prepare_polystrata(
            tmp_path / "out-evil",
            data_dir=tmp_path / "data-evil",
            source=str(evil_tar),
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    assert exc_info.value.reason == "unsafe_archive_entry"

    # Reject plaintext HTTP URL
    with pytest.raises(PreparationError) as exc_http:
        polystrata_prepare.prepare_polystrata(
            tmp_path / "out-http",
            source="http://example.test/polystrata.tar.gz",
        )
    assert exc_http.value.reason == "invalid_engine_source"


@pytest.mark.skipif(os.name == "nt", reason="POSIX SIGTERM process-group cancellation test")
def test_cancellation_cleans_up_process_tree_and_preserves_partial_downloads(tmp_path: Path):
    """Spawn a real CLI subprocess running fake setup.py with a grandchild; SIGTERM must kill both."""
    child_pid_file = tmp_path / "grandchild.pid"
    slow_setup = f"""\
import subprocess, sys, time
from pathlib import Path

data_dir = Path(sys.argv[sys.argv.index("--data-dir") + 1])
(data_dir / "partial.gguf.part").write_bytes(b"keep-for-resume")
child = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(60)"])
Path({str(child_pid_file)!r}).write_text(str(child.pid), encoding="utf-8")
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
    out = tmp_path / "ps-cancel"
    data = tmp_path / "data-cancel"
    # Pre-seed extracted tree with lightweight venv so the subprocess starts setup.py immediately
    # without waiting on full ensurepip venv creation.
    out.mkdir()
    (out / "setup.py").write_text(slow_setup, encoding="utf-8")
    (out / "serve").mkdir()
    (out / "serve" / "server.py").write_text("# server\n", encoding="utf-8")
    _make_fake_venv(out)

    # Run via python -c invoking execute_polystrata_prepare with mocked preflight so the test
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


def test_cli_argument_parsing_and_application_error_result(tmp_path: Path):
    args = parser().parse_args(
        [
            "engine",
            "prepare-polystrata",
            "--out",
            str(tmp_path / "ps"),
            "--data-dir",
            str(tmp_path / "data"),
            "--family",
            "qwen36",
            "--model",
            "UD-Q4_K_M",
            "--context",
            "32768",
            "--backend",
            "metal",
            "--model-source",
            "modelscope",
            "--gguf-path",
            str(tmp_path / "m.gguf"),
            "--source",
            "https://example.test/polystrata.tar.gz",
            "--proxy-origin",
            "http://127.0.0.1:7890",
            "--resume",
            "--progress-stream",
            "--timeout-seconds",
            "900",
        ]
    )
    req = build_request(args)
    assert req.command == "engine prepare-polystrata"
    assert req.family == "qwen36"
    assert req.polystrata_model == "UD-Q4_K_M"
    assert req.context_size == 32768
    assert req.polystrata_backend == "metal"
    assert req.polystrata_model_source == "modelscope"
    assert req.gguf_path == (tmp_path / "m.gguf").absolute()
    assert req.resume_transfer is True
    assert req.progress_stream is True
    assert req.engine_timeout_seconds == 900.0

    # Application handler maps PreparationError to blocked CommandResult with details
    bad_args = parser().parse_args(
        [
            "engine",
            "prepare-polystrata",
            "--out",
            str(tmp_path / "ps-bad"),
            "--source",
            "http://insecure.example/archive.tar.gz",
        ]
    )
    code, result = execute_polystrata_prepare(build_request(bad_args))
    assert code == 2
    assert result.status == "blocked"
    assert result.limitations == ("invalid_engine_source",)


def test_verification_fails_when_engine_binary_or_config_missing(tmp_path: Path):
    # setup.py exits 0 but writes no engine binary
    no_engine_archive = _tar_gz_file(
        tmp_path / "no-engine.tar.gz",
        [
            ("file", "setup.py", b"print('done')\n"),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
        ],
    )
    with pytest.raises(PreparationError) as exc_eng:
        polystrata_prepare.prepare_polystrata(
            tmp_path / "out-no-eng",
            data_dir=tmp_path / "data-no-eng",
            source=str(no_engine_archive),
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            venv_creator=_make_fake_venv,
        )
    assert exc_eng.value.reason == "local_engine_polystrata_engine_missing"

    # setup.py creates engine binary but no strata-*.json config
    no_cfg_setup = """\
import os
from pathlib import Path
root = Path(__file__).resolve().parent
d = root / "engine-poly"
d.mkdir(exist_ok=True)
b = d / ("strata-poly.exe" if os.name == "nt" else "strata-poly")
b.write_text("#!/bin/sh\\n", encoding="utf-8")
if os.name != "nt":
    os.chmod(b, 0o755)
"""
    no_cfg_archive = _tar_gz_file(
        tmp_path / "no-cfg.tar.gz",
        [
            ("file", "setup.py", no_cfg_setup.encode("utf-8")),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
        ],
    )
    with pytest.raises(PreparationError) as exc_cfg:
        polystrata_prepare.prepare_polystrata(
            tmp_path / "out-no-cfg",
            data_dir=tmp_path / "data-no-cfg",
            source=str(no_cfg_archive),
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            venv_creator=_make_fake_venv,
        )
    assert exc_cfg.value.reason == "polystrata_config_missing"


def test_preflight_family_without_model_uses_smallest_size_in_family(tmp_path: Path):
    # qwen36 UD-Q2_K_XL needs ~14 GiB + 2 GiB runtime = ~16 GiB, whereas UD-Q4_K_M needs ~26 GiB.
    # With 20 GiB free and only --family qwen36 (no --model), preflight must NOT falsely block.
    report = run_polystrata_preflight(
        tmp_path / "out-fam",
        tmp_path / "data-fam",
        family="qwen36",
        model=None,
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(20 * GIB),
    )
    assert report["blockers"] == []

