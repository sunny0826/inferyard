"""Isolated process-group runner and progress stream parser for PolyStrata setup.py."""

from __future__ import annotations

import codecs
import os
from pathlib import Path
import queue
import re
import signal
import subprocess
import threading
import time

from inferyard.provisioning.acquire_errors import failure_details
from inferyard.provisioning.http_client import validate_proxy_origin
from inferyard.provisioning.polystrata_archive import SETUP_LOG_NAME
from inferyard.provisioning.polystrata_preflight import sanitize_log_line
from inferyard.provisioning.preparation_io import PreparationError, linked
from inferyard.provisioning.transfer_progress import ProgressEmitter

_MAX_LOG_BYTES = 16 * 1024 * 1024
_STEP_RE = re.compile(r"^===\s*Step\s+(\d+)\s*:\s*(.+?)\s*===$", re.IGNORECASE)
_DOWNLOAD_LINE_RE = re.compile(
    r"^\s*(.+?):\s*([0-9]+(?:\.[0-9]+)?)\s*/\s*([0-9]+(?:\.[0-9]+)?)\s*"
    r"(GB|GiB|MB|MiB|KB|KiB)\s*\(([0-9]+(?:\.[0-9]+)?)%\)",
    re.IGNORECASE,
)
_CMAKE_PCT_RE = re.compile(r"^\[\s*(\d{1,3})%\]\s*(.*)$")
_NINJA_STEP_RE = re.compile(r"^\[(\d+)/(\d+)\]\s*(.*)$")
_UNIT_MULTIPLIERS = {
    "kb": 1_000,
    "kib": 1024,
    "mb": 1_000_000,
    "mib": 1024**2,
    "gb": 1_000_000_000,
    "gib": 1024**3,
}


class _SetupCancelled(BaseException):
    """Internal signal bridge raised when SIGTERM arrives during setup.py."""


def terminate_process_group(proc: subprocess.Popen) -> None:
    if proc.poll() is not None:
        return
    if os.name == "nt":
        try:
            subprocess.run(
                ["taskkill", "/T", "/F", "/PID", str(proc.pid)],
                stdin=subprocess.DEVNULL,
                capture_output=True,
                timeout=5,
                check=False,
            )
        except (OSError, subprocess.SubprocessError):
            pass
        try:
            proc.kill()
        except OSError:
            pass
    else:
        try:
            pgid = os.getpgid(proc.pid)
        except OSError:
            pgid = None
        if pgid is not None:
            try:
                os.killpg(pgid, signal.SIGTERM)
            except OSError:
                pass
        else:
            try:
                proc.terminate()
            except OSError:
                pass
        deadline = time.monotonic() + 3.0
        while time.monotonic() < deadline:
            if proc.poll() is not None:
                return
            time.sleep(0.05)
        if pgid is not None:
            try:
                os.killpg(pgid, signal.SIGKILL)
            except OSError:
                pass
        else:
            try:
                proc.kill()
            except OSError:
                pass
    try:
        proc.wait(timeout=3.0)
    except subprocess.TimeoutExpired:
        pass


def parse_setup_line(
    line: str,
    current_stage: str,
    current_step: int | None,
    emitter: ProgressEmitter,
) -> tuple[str, int | None]:
    stripped = line.strip()
    if not stripped:
        return current_stage, current_step
    step_match = _STEP_RE.match(stripped)
    if step_match is not None:
        step_num = int(step_match.group(1))
        title = step_match.group(2).strip()
        next_stage = (
            "downloading_model"
            if step_num == 5 or "download" in title.lower()
            else "setup_engine"
        )
        emitter.update(
            next_stage, 0, None, step=step_num, step_total=7, detail=title, force=True
        )
        return next_stage, step_num

    dl_match = _DOWNLOAD_LINE_RE.match(stripped)
    if dl_match is not None:
        label = dl_match.group(1).strip()
        mult = _UNIT_MULTIPLIERS[dl_match.group(4).lower()]
        have_bytes = int(float(dl_match.group(2)) * mult)
        total_bytes = max(1, int(float(dl_match.group(3)) * mult))
        emitter.update(
            "downloading_model",
            have_bytes,
            total_bytes,
            step=current_step or 5,
            step_total=7,
            percent=float(dl_match.group(5)),
            detail=label,
        )
        return "downloading_model", current_step or 5

    cmake_match = _CMAKE_PCT_RE.match(stripped)
    if cmake_match is not None:
        emitter.update(
            "setup_engine",
            0,
            None,
            step=current_step or 4,
            step_total=7,
            percent=float(cmake_match.group(1)),
            detail=cmake_match.group(2).strip() or stripped,
        )
        return "setup_engine", current_step or 4

    ninja_match = _NINJA_STEP_RE.match(stripped)
    if ninja_match is not None:
        done, total = int(ninja_match.group(1)), int(ninja_match.group(2))
        if total > 0:
            emitter.update(
                "setup_engine",
                0,
                None,
                step=current_step or 4,
                step_total=7,
                percent=(100.0 * done) / total,
                detail=ninja_match.group(3).strip() or stripped,
            )
            return "setup_engine", current_step or 4

    return current_stage, current_step


def build_setup_command(
    out: Path,
    venv_python: Path,
    data_dir: Path,
    *,
    system: str,
    family: str | None,
    model: str | None,
    context: int | None,
    backend: str | None,
    model_source: str | None,
    gguf_path: Path | None,
) -> list[str]:
    cmd = [
        str(venv_python),
        "-u",
        str(out / "setup.py"),
        "--yes",
        "--no-start",
        "--data-dir",
        str(data_dir),
    ]
    if family:
        cmd.extend(["--family", family])
    if model:
        cmd.extend(["--model", model])
    if context is not None:
        cmd.extend(["--context", str(context)])
    if model_source:
        cmd.extend(["--source", model_source])
    if backend in ("cuda", "hip") and system != "Darwin":
        cmd.extend(["--backend", backend])
    if gguf_path is not None:
        if system == "Darwin":
            gguf_dir = gguf_path if gguf_path.is_dir() else gguf_path.parent
            cmd.extend(["--gguf-dir", str(gguf_dir)])
        else:
            cmd.extend(["--custom-gguf", str(gguf_path)])
    return cmd


def run_setup_process(
    out: Path,
    venv_python: Path,
    data_dir: Path,
    *,
    system: str,
    family: str | None,
    model: str | None,
    context: int | None,
    backend: str | None,
    model_source: str | None,
    gguf_path: Path | None,
    proxy_origin: str | None,
    timeout_seconds: float,
    emitter: ProgressEmitter,
) -> Path:
    cmd = build_setup_command(
        out,
        venv_python,
        data_dir,
        system=system,
        family=family,
        model=model,
        context=context,
        backend=backend,
        model_source=model_source,
        gguf_path=gguf_path,
    )
    env = dict(os.environ)
    env["PYTHONUNBUFFERED"] = "1"
    env["PYTHONUTF8"] = "1"
    if model_source:
        env["STRATA_SOURCE"] = model_source
    if proxy_origin:
        validated = validate_proxy_origin(proxy_origin)
        assert validated is not None
        for key in ("HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"):
            env[key] = validated

    log_path = out / SETUP_LOG_NAME
    if os.path.lexists(log_path) and linked(log_path):
        raise PreparationError("output_exists")

    emitter.update(
        "setup_engine", 0, None, step=3, step_total=4, detail="running PolyStrata setup.py", force=True
    )

    popen_kwargs: dict[str, object] = {
        "cwd": str(out),
        "env": env,
        "stdin": subprocess.DEVNULL,
        "stdout": subprocess.PIPE,
        "stderr": subprocess.STDOUT,
        "bufsize": 0,
    }
    if os.name == "nt":
        popen_kwargs["creationflags"] = subprocess.CREATE_NEW_PROCESS_GROUP
    else:
        popen_kwargs["start_new_session"] = True

    install_sigterm = (
        hasattr(signal, "SIGTERM") and threading.current_thread() is threading.main_thread()
    )
    previous_sigterm = None
    if install_sigterm:
        def _handle_sigterm(_signum, _frame):
            raise _SetupCancelled

        previous_sigterm = signal.signal(signal.SIGTERM, _handle_sigterm)

    proc: subprocess.Popen | None = None
    current_stage = "setup_engine"
    current_step: int | None = None
    log_bytes = 0
    truncated_notice_written = False

    try:
        proc = subprocess.Popen(cmd, **popen_kwargs)
        assert proc.stdout is not None
        stdout_fd = proc.stdout.fileno()
        chunks: queue.Queue[bytes | None] = queue.Queue()

        def _reader() -> None:
            try:
                while True:
                    data = os.read(stdout_fd, 4096)
                    if not data:
                        break
                    chunks.put(data)
            except OSError:
                pass
            finally:
                chunks.put(None)

        reader_thread = threading.Thread(target=_reader, daemon=True)
        reader_thread.start()

        decoder = codecs.getincrementaldecoder("utf-8")("replace")
        pending = ""
        deadline = time.monotonic() + float(timeout_seconds)

        with log_path.open("w", encoding="utf-8", errors="replace") as log_file:
            def _handle_line(raw_line: str) -> None:
                nonlocal current_stage, current_step, log_bytes, truncated_notice_written
                cleaned = sanitize_log_line(raw_line)
                if cleaned:
                    encoded_len = len(cleaned.encode("utf-8")) + 1
                    if log_bytes + encoded_len <= _MAX_LOG_BYTES:
                        log_file.write(cleaned + "\n")
                        log_file.flush()
                        log_bytes += encoded_len
                    elif not truncated_notice_written:
                        log_file.write("[log truncated at 16 MiB]\n")
                        log_file.flush()
                        truncated_notice_written = True
                current_stage, current_step = parse_setup_line(
                    cleaned, current_stage, current_step, emitter
                )

            eof = False
            while not eof:
                if time.monotonic() > deadline:
                    terminate_process_group(proc)
                    raise PreparationError(
                        "polystrata_setup_timeout",
                        code=2,
                        details={
                            **failure_details(current_stage, "timeout"),
                            "log_path": str(log_path),
                        },
                    )
                try:
                    chunk = chunks.get(timeout=0.2)
                except queue.Empty:
                    continue
                if chunk is None:
                    eof = True
                    text = decoder.decode(b"", final=True)
                else:
                    text = decoder.decode(chunk, final=False)
                if not text:
                    continue
                pending += text
                while True:
                    idx_n = pending.find("\n")
                    idx_r = pending.find("\r")
                    if idx_n == -1 and idx_r == -1:
                        break
                    cut = idx_n if idx_r == -1 else idx_r if idx_n == -1 else min(idx_n, idx_r)
                    line = pending[:cut]
                    pending = pending[cut + 1 :]
                    _handle_line(line)
            if pending:
                _handle_line(pending)

        reader_thread.join(timeout=2.0)
        rc = proc.wait(timeout=5.0)
    except (KeyboardInterrupt, _SetupCancelled):
        if proc is not None:
            terminate_process_group(proc)
        raise PreparationError(
            "polystrata_setup_cancelled",
            code=130,
            details={**failure_details(current_stage, "cancelled"), "log_path": str(log_path)},
        ) from None
    finally:
        if install_sigterm and previous_sigterm is not None:
            signal.signal(signal.SIGTERM, previous_sigterm)
        if proc is not None and proc.stdout is not None:
            try:
                proc.stdout.close()
            except OSError:
                pass

    if rc != 0:
        raise PreparationError(
            "polystrata_setup_failed",
            code=2,
            details={
                **failure_details(current_stage, "polystrata_setup_failed"),
                "exit_code": rc,
                "log_path": str(log_path),
            },
        )
    return log_path
