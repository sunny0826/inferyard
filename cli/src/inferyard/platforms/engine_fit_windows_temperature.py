"""Bounded NVIDIA temperature reads; no ACPI or CPU-temperature substitution."""

import csv
import io
import math
import os
import shutil
import subprocess
import threading
import time

from inferyard.platforms.identity import PreflightError

OUTPUT_LIMIT = 64 * 1024


def _read_query(executable):
    deadline = time.monotonic() + 2.0
    process = subprocess.Popen(
        [
            executable,
            "--query-gpu=index,temperature.gpu",
            "--format=csv,noheader,nounits",
        ],
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
    )
    output, done = {}, threading.Event()

    def read():
        try:
            output["data"] = process.stdout.read(OUTPUT_LIMIT + 1)
        except OSError:
            output["failed"] = True
        finally:
            done.set()

    reader = threading.Thread(target=read, daemon=True)
    reader.start()
    try:
        if not done.wait(max(0, deadline - time.monotonic())):
            raise PreflightError("nvidia_temperature_query_timeout")
        if output.get("failed"):
            raise PreflightError("nvidia_temperature_query_unreadable")
        raw = output["data"]
        if len(raw) > OUTPUT_LIMIT:
            raise PreflightError("nvidia_temperature_output_too_large")
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise PreflightError("nvidia_temperature_query_timeout")
        if process.wait(timeout=remaining) != 0:
            raise PreflightError("nvidia_temperature_query_failed")
        return raw.decode("ascii", "strict")
    except subprocess.TimeoutExpired as exc:
        raise PreflightError("nvidia_temperature_query_timeout") from exc
    finally:
        if process.poll() is None:
            process.kill()  # Only our private read-only query, never a model service.
        process.wait(timeout=1.0)
        reader.join(timeout=1.0)
        # Do not block on a pipe inherited by an unexpected descendant.
        if not reader.is_alive():
            process.stdout.close()


def _values(raw):
    values, seen = [], set()
    for row in csv.reader(io.StringIO(raw)):
        if len(row) != 2:
            raise ValueError("nvidia_temperature_invalid_output")
        index, reported = (cell.strip() for cell in row)
        if not index.isascii() or not index.isdigit() or len(index) > 5:
            raise ValueError("nvidia_temperature_invalid_output")
        index = int(index)
        if index in seen or len(seen) >= 256:
            raise ValueError("nvidia_temperature_invalid_output")
        seen.add(index)
        reason, value = None, None
        if reported in ("N/A", "[N/A]", "Not Supported", "[Not Supported]"):
            reason = "nvidia_temperature_not_reported"
        else:
            value = float(reported)
            if not math.isfinite(value) or not -273.15 <= value <= 200:
                raise ValueError("nvidia_temperature_invalid_output")
        values.append((f"nvidia-smi:gpu:{index}:temperature.gpu", value, reason))
    if not values:
        raise ValueError("nvidia_temperature_no_devices")
    return values


class WindowsTemperature:
    sources = [{"metric_name": "temperature"}]

    def collect(self, phase, request_id):
        started = time.monotonic_ns()
        try:
            executable = shutil.which("nvidia-smi")
            if executable is None:
                raise PreflightError("nvidia_temperature_tool_unavailable")
            values = _values(_read_query(executable))
        except (
            OSError,
            UnicodeError,
            ValueError,
            PreflightError,
            subprocess.TimeoutExpired,
        ) as exc:
            reason = (
                str(exc)
                if isinstance(exc, PreflightError)
                else ("nvidia_temperature_query_unavailable")
            )
            values = [("nvidia-smi:unavailable", None, reason)]
        finished = time.monotonic_ns()
        return [
            {
                "collector": "windows-nvidia-temperature.v1",
                "server_pid": None,
                "process_start_ticks": None,
                "phase": phase,
                "request_id": request_id,
                "metric_name": "temperature",
                "source": source,
                "unit": "celsius",
                "semantics": "gpu_temperature_reported",
                "raw_value": value,
                "value": value,
                "missing_reason": reason,
                "read_started_ns": started,
                "read_finished_ns": finished,
            }
            for source, value, reason in values
        ]
