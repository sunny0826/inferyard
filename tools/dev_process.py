"""Cross-platform process identity checks and graceful stop/launch handoff."""

from __future__ import annotations

import ctypes
import errno
import json
import os
import secrets
import signal
import subprocess
import sys
import tempfile
import time
from datetime import UTC, datetime
from pathlib import Path


class DevError(RuntimeError):
    pass


def atomic_write_json(path: Path, value: dict[str, object]) -> None:
    if path.is_symlink():
        raise DevError(f"Refusing to write through symlink: {path}")
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp_name = tempfile.mkstemp(prefix=f".{path.name}-", dir=path.parent)
    tmp_path = Path(tmp_name)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(value, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(tmp_path, path)
    finally:
        tmp_path.unlink(missing_ok=True)


def normalize_exe_path(path_text: str) -> str:
    return os.path.normcase(str(Path(path_text).resolve()))


def process_identity(pid: int) -> tuple[str, str] | None:
    """Return (canonical_executable_path, start_id) for pid, or None if exited."""
    if pid <= 0:
        return None
    if os.name == "nt":
        return _process_identity_windows(pid)
    if sys.platform == "darwin":
        return _process_identity_macos(pid)
    return _process_identity_linux(pid)


def _process_identity_macos(pid: int) -> tuple[str, str] | None:
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return None
    except PermissionError as exc:
        raise DevError(f"Cannot inspect process {pid}: permission denied") from exc
    lib = ctypes.CDLL("/usr/lib/libSystem.B.dylib", use_errno=True)
    buf = ctypes.create_string_buffer(4096)
    size = lib.proc_pidpath(ctypes.c_int(pid), buf, ctypes.c_uint32(len(buf)))
    if size <= 0:
        err = ctypes.get_errno()
        if err == errno.ESRCH:
            return None
        raise DevError(f"Cannot resolve executable path for PID {pid} (errno {err})")
    exe = normalize_exe_path(buf.value.decode("utf-8", errors="strict"))
    ps = subprocess.run(
        ["ps", "-p", str(pid), "-o", "lstart="],
        capture_output=True,
        text=True,
        check=False,
    )
    if ps.returncode != 0 or not ps.stdout.strip():
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            return None
        raise DevError(f"Cannot inspect start time for PID {pid}")
    return exe, ps.stdout.strip()


def _process_identity_linux(pid: int) -> tuple[str, str] | None:
    proc = Path(f"/proc/{pid}")
    try:
        exe = normalize_exe_path(os.readlink(proc / "exe"))
        stat = (proc / "stat").read_text(encoding="utf-8")
    except (FileNotFoundError, ProcessLookupError):
        return None
    except OSError as exc:
        raise DevError(f"Cannot inspect process {pid}: {exc}") from exc
    end_comm = stat.rfind(")")
    if end_comm < 0:
        raise DevError(f"Unreadable /proc/{pid}/stat")
    fields = stat[end_comm + 2 :].split()
    if len(fields) < 20:
        raise DevError(f"Incomplete /proc/{pid}/stat")
    return exe, fields[19]


def _process_identity_windows(pid: int) -> tuple[str, str] | None:
    process_query_limited = 0x1000
    error_invalid_parameter = 87
    kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
    handle = kernel32.OpenProcess(process_query_limited, False, pid)
    if not handle:
        err = ctypes.get_last_error()
        if err == error_invalid_parameter:
            return None
        raise DevError(f"Cannot open process {pid} (winerr {err})")
    try:
        buf = ctypes.create_unicode_buffer(4096)
        size = ctypes.c_uint32(len(buf))
        if not kernel32.QueryFullProcessImageNameW(handle, 0, buf, ctypes.byref(size)):
            raise DevError(f"Cannot query executable path for PID {pid}")

        class FILETIME(ctypes.Structure):
            _fields_ = [("dwLowDateTime", ctypes.c_uint32), ("dwHighDateTime", ctypes.c_uint32)]

        creation, exit_t, kernel_t, user_t = FILETIME(), FILETIME(), FILETIME(), FILETIME()
        if not kernel32.GetProcessTimes(
            handle,
            ctypes.byref(creation),
            ctypes.byref(exit_t),
            ctypes.byref(kernel_t),
            ctypes.byref(user_t),
        ):
            raise DevError(f"Cannot query start time for PID {pid}")
        start_ticks = (creation.dwHighDateTime << 32) | creation.dwLowDateTime
        return normalize_exe_path(buf.value), str(start_ticks)
    finally:
        kernel32.CloseHandle(handle)


def identity_matches(pid: int, expected_binary: str, expected_start_id: str) -> bool:
    if not expected_binary or not expected_start_id:
        raise DevError("Recorded development session is missing executable or start identity")
    actual = process_identity(pid)
    if actual is None:
        return False
    exe, start_id = actual
    return exe == normalize_exe_path(expected_binary) and start_id == expected_start_id


def verify_clean_shutdown(data: dict[str, object]) -> None:
    status_path_raw = data.get("status_file")
    if isinstance(status_path_raw, str) and status_path_raw:
        status_path = Path(status_path_raw)
        if status_path.is_file() and not status_path.is_symlink():
            try:
                status = json.loads(status_path.read_text(encoding="utf-8"))
            except (OSError, ValueError) as exc:
                raise DevError(f"Unreadable Studio shutdown status: {status_path}") from exc
            if isinstance(status, dict) and status.get("state") == "shutdown_failed":
                detail = status.get("error", "unknown shutdown error")
                raise DevError(
                    f"Previous development Studio failed to shut down cleanly ({detail}); inspect {data.get('log', '')}"
                )
    profile_raw = data.get("profile")
    if isinstance(profile_raw, str) and profile_raw:
        owner_file = Path(profile_raw) / "workspace" / "logs" / "engine-owner.json"
        if owner_file.exists() or owner_file.is_symlink():
            raise DevError(
                f"Engine ownership record still exists at {owner_file}; stop or clear the orphaned engine before switching"
            )


def stop_recorded(state_dir: Path, *, timeout: float = 30.0) -> bool:
    session_file = state_dir / "session.json"
    if not session_file.exists() and not session_file.is_symlink():
        return False
    if session_file.is_symlink():
        raise DevError(f"Refusing to read symlink session record: {session_file}")
    try:
        data = json.loads(session_file.read_text(encoding="utf-8"))
        if not isinstance(data, dict):
            raise ValueError("session record is not an object")
        pid = int(data["pid"])
        binary = str(data.get("binary", ""))
        start_id = str(data.get("start_id", ""))
    except (OSError, ValueError, KeyError, TypeError) as exc:
        raise DevError(f"Invalid development session record: {session_file}") from exc
    if not identity_matches(pid, binary, start_id):
        verify_clean_shutdown(data)
        session_file.unlink(missing_ok=True)
        return False
    if os.name == "nt":
        subprocess.run(["taskkill", "/PID", str(pid)], capture_output=True, check=False)
    else:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            verify_clean_shutdown(data)
            session_file.unlink(missing_ok=True)
            return True
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if not identity_matches(pid, binary, start_id):
            verify_clean_shutdown(data)
            session_file.unlink(missing_ok=True)
            return True
        time.sleep(0.2)
    log_path = data.get("log", "")
    raise DevError(
        f"Development Studio (PID {pid}) did not exit within {int(timeout)}s; inspect {log_path}"
    )


def _read_status(status_file: Path) -> dict[str, object] | None:
    if not status_file.is_file() or status_file.is_symlink():
        return None
    try:
        data = json.loads(status_file.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None
    return data if isinstance(data, dict) else None


def launch_studio(
    root: Path,
    state_dir: Path,
    binary: Path,
    cli: Path,
    profile: Path,
    assets: Path,
    label: str,
    *,
    ready_timeout: float = 15.0,
) -> dict[str, object]:
    logs_dir = state_dir / "logs"
    logs_dir.mkdir(parents=True, exist_ok=True)
    stamp = datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ")
    token = secrets.token_hex(4)
    log_path = logs_dir / f"studio-{stamp}-{token}.log"
    status_file = logs_dir / f"studio-{stamp}-{token}.status.json"
    args = [
        str(binary),
        "--dev-profile",
        str(profile),
        "--dev-assets",
        str(assets),
        "--dev-cli",
        str(cli),
        "--dev-label",
        label,
        "--dev-status",
        str(status_file),
    ]
    kwargs: dict[str, object] = {"cwd": root}
    if os.name == "nt":
        kwargs["creationflags"] = getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0)
    else:
        kwargs["start_new_session"] = True
    with log_path.open("ab") as stream:
        proc = subprocess.Popen(args, stdout=stream, stderr=subprocess.STDOUT, **kwargs)
    deadline = time.monotonic() + ready_timeout
    ready = False
    while time.monotonic() < deadline:
        code = proc.poll()
        if code is not None:
            raise DevError(f"Development Studio exited early with code {code}; inspect {log_path}")
        status = _read_status(status_file)
        if status and status.get("state") == "ready" and status.get("pid") == proc.pid:
            ready = True
            break
        time.sleep(0.1)
    if not ready:
        if proc.poll() is None:
            try:
                proc.terminate()
                proc.wait(timeout=5)
            except (OSError, subprocess.TimeoutExpired):
                pass
        raise DevError(
            f"Development Studio (PID {proc.pid}) did not become ready within {int(ready_timeout)}s; inspect {log_path}"
        )
    identity = process_identity(proc.pid)
    if identity is None:
        raise DevError(
            f"Development Studio (PID {proc.pid}) exited immediately after ready; inspect {log_path}"
        )
    exe, start_id = identity
    record: dict[str, object] = {
        "pid": proc.pid,
        "binary": exe,
        "start_id": start_id,
        "cli": str(cli),
        "profile": str(profile),
        "assets": str(assets),
        "status_file": str(status_file),
        "log": str(log_path),
    }
    atomic_write_json(state_dir / "session.json", record)
    return record
