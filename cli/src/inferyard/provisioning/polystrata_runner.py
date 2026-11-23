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
from inferyard.provisioning.polystrata_archive import CONFIG_DIR_NAME, SETUP_LOG_NAME
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
_MODELSCOPE_NO_PROXY_HOSTS = ("modelscope.cn", ".modelscope.cn")


def _merge_no_proxy(
    existing_upper: str | None, existing_lower: str | None, additions: tuple[str, ...]
) -> str:
    active = existing_lower if existing_lower is not None else existing_upper
    if active is not None and active.strip() == "*":
        return "*"
    merged: list[str] = []
    seen: set[str] = set()
    for is_upper, raw in ((True, existing_upper), (False, existing_lower)):
        if not raw:
            continue
        for part in raw.split(","):
            token = part.strip()
            if is_upper and existing_lower is not None and token == "*":
                continue
            key = token.casefold()
            if token and key not in seen:
                seen.add(key)
                merged.append(token)
    for host in additions:
        key = host.casefold()
        if key not in seen:
            seen.add(key)
            merged.append(host)
    return ",".join(merged)


class _SetupCancelled(BaseException):
    """Internal signal bridge raised when SIGTERM arrives during setup.py."""


def _pgid_alive(pgid: int) -> bool:
    try:
        os.killpg(pgid, 0)
        return True
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    except OSError:
        return False


def terminate_process_group(proc: subprocess.Popen, pgid: int | None = None) -> None:
    if os.name == "nt":
        if proc.poll() is None:
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
        try:
            proc.wait(timeout=3.0)
        except subprocess.TimeoutExpired:
            pass
        return

    group_id = pgid
    if group_id is None:
        try:
            group_id = os.getpgid(proc.pid)
        except OSError:
            group_id = None

    if group_id is not None:
        try:
            os.killpg(group_id, signal.SIGTERM)
        except OSError:
            pass
        deadline = time.monotonic() + 3.0
        while time.monotonic() < deadline:
            proc.poll()
            if proc.returncode is not None and not _pgid_alive(group_id):
                break
            time.sleep(0.05)
        if _pgid_alive(group_id) or proc.poll() is None:
            try:
                os.killpg(group_id, signal.SIGKILL)
            except OSError:
                pass
            kill_deadline = time.monotonic() + 3.0
            while time.monotonic() < kill_deadline:
                proc.poll()
                if proc.returncode is not None and not _pgid_alive(group_id):
                    break
                time.sleep(0.05)
    elif proc.poll() is None:
        try:
            proc.terminate()
        except OSError:
            pass
        deadline = time.monotonic() + 3.0
        while time.monotonic() < deadline:
            if proc.poll() is not None:
                break
            time.sleep(0.05)
        if proc.poll() is None:
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
    base_step = 4 if (current_step or 0) >= 4 else 3
    step_match = _STEP_RE.match(stripped)
    if step_match is not None:
        step_num = int(step_match.group(1))
        title = step_match.group(2).strip()
        detail = f"Step {step_num}: {title}"
        is_model_download = step_num == 5 or (
            "download" in title.lower() and (step_num >= 5 or "model" in title.lower())
        )
        if is_model_download:
            next_stage, outer_step = "downloading_model", 4
        elif (
            step_num >= 6
            or current_stage in ("downloading_model", "verifying")
            or base_step >= 4
        ):
            next_stage, outer_step = "verifying", 4
        else:
            next_stage, outer_step = "setup_engine", 3
        emitter.update(
            next_stage,
            0,
            None,
            step=outer_step,
            step_total=4,
            detail=detail,
            force=True,
        )
        return next_stage, outer_step

    dl_match = _DOWNLOAD_LINE_RE.match(stripped)
    if dl_match is not None:
        label = dl_match.group(1).strip()
        mult = _UNIT_MULTIPLIERS[dl_match.group(4).lower()]
        have_bytes = int(float(dl_match.group(2)) * mult)
        total_bytes = max(1, int(float(dl_match.group(3)) * mult))
        dl_stage = (
            "downloading_model"
            if (current_stage == "downloading_model" or base_step >= 4)
            else "setup_engine"
        )
        outer_step = 4 if dl_stage == "downloading_model" else 3
        emitter.update(
            dl_stage,
            have_bytes,
            total_bytes,
            step=outer_step,
            step_total=4,
            percent=float(dl_match.group(5)),
            detail=label,
        )
        return dl_stage, outer_step

    cmake_match = _CMAKE_PCT_RE.match(stripped)
    if cmake_match is not None:
        stage = current_stage if base_step >= 4 else "setup_engine"
        emitter.update(
            stage,
            0,
            None,
            step=base_step,
            step_total=4,
            percent=float(cmake_match.group(1)),
            detail=cmake_match.group(2).strip() or stripped,
        )
        return stage, base_step

    ninja_match = _NINJA_STEP_RE.match(stripped)
    if ninja_match is not None:
        done, total = int(ninja_match.group(1)), int(ninja_match.group(2))
        if total > 0:
            stage = current_stage if base_step >= 4 else "setup_engine"
            emitter.update(
                stage,
                0,
                None,
                step=base_step,
                step_total=4,
                percent=(100.0 * done) / total,
                detail=ninja_match.group(3).strip() or stripped,
            )
            return stage, base_step

    if current_stage == "downloading_model":
        lower = stripped.lower()
        if (
            lower.startswith("checking ")
            or lower.startswith("verifying ")
            or "sha-256" in lower
            or "sha256" in lower
        ):
            emitter.update(
                "verifying",
                0,
                None,
                step=4,
                step_total=4,
                detail=stripped,
                force=True,
            )
            return "verifying", 4

    if current_stage in ("setup_engine", "verifying") and any(ch.isalnum() for ch in stripped):
        emitter.update(
            current_stage,
            0,
            None,
            step=base_step,
            step_total=4,
            detail=stripped,
            force=False,
        )
        return current_stage, base_step

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
            cmd.extend(["--gguf-dir", str(gguf_dir), "--no-download-models"])
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
    config_dir = out / CONFIG_DIR_NAME
    if os.path.lexists(config_dir) and (linked(config_dir) or not config_dir.is_dir()):
        raise PreparationError("output_exists")
    config_dir.mkdir(parents=True, exist_ok=True)

    env = dict(os.environ)
    env["PYTHONUNBUFFERED"] = "1"
    env["PYTHONUTF8"] = "1"
    # Isolate PolyStrata's settings.json so setup.py never reads/mutates user-global install lists.
    if os.name == "nt":
        env["APPDATA"] = str(config_dir)
    else:
        env["XDG_CONFIG_HOME"] = str(config_dir)
    if model_source:
        env["STRATA_SOURCE"] = model_source
    if proxy_origin:
        validated = validate_proxy_origin(proxy_origin)
        assert validated is not None
        for key in ("HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"):
            env[key] = validated
        if (model_source or "").strip().lower() == "modelscope":
            merged_no_proxy = _merge_no_proxy(
                env.get("NO_PROXY"), env.get("no_proxy"), _MODELSCOPE_NO_PROXY_HOSTS
            )
            env["NO_PROXY"] = merged_no_proxy
            env["no_proxy"] = merged_no_proxy

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
    pgid: int | None = None
    current_stage = "setup_engine"
    current_step: int | None = 3
    log_bytes = 0
    truncated_notice_written = False

    try:
        proc = subprocess.Popen(cmd, **popen_kwargs)
        if os.name != "nt":
            pgid = proc.pid
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
                    terminate_process_group(proc, pgid)
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
            terminate_process_group(proc, pgid)
        raise PreparationError(
            "polystrata_setup_cancelled",
            code=130,
            details={**failure_details(current_stage, "cancelled"), "log_path": str(log_path)},
        ) from None
    finally:
        if proc is not None and (
            proc.poll() is None or (pgid is not None and _pgid_alive(pgid))
        ):
            terminate_process_group(proc, pgid)
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
