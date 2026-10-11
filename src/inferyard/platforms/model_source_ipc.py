"""Bounded child output reader; pipe IO never owns the parent's timeout clock."""

import math
import re
import threading
from pathlib import Path

from inferyard.contracts.validation import strict_json_loads
from inferyard.platforms.model_source_progress import STAGES

FRAME_LIMIT = 16 * 1024


def result_details(value, max_bytes, filename):
    if not isinstance(value, dict) or set(value) != {
        "out",
        "model",
        "source_record",
        "bytes",
        "sha256",
        "ready_to_run",
        "model_requests_sent",
    }:
        raise ValueError
    if type(value["bytes"]) is not int or not 0 < value["bytes"] <= max_bytes:
        raise ValueError
    if not isinstance(value["sha256"], str) or not re.fullmatch("[a-f0-9]{64}", value["sha256"]):
        raise ValueError
    if value["ready_to_run"] is not False or type(value["model_requests_sent"]) is not int:
        raise ValueError
    if value["model_requests_sent"] != 0:
        raise ValueError
    for key in ("out", "model", "source_record"):
        if not isinstance(value[key], str) or not Path(value[key]).is_absolute():
            raise ValueError
    if Path(value["model"]) != Path(value["out"]) / filename:
        raise ValueError
    if Path(value["source_record"]) != Path(value["out"]) / "source.json":
        raise ValueError
    return value


def progress_frame(value, max_bytes, started):
    if not isinstance(value, dict) or set(value) != {
        "stage",
        "activity",
        "network_at",
        "received",
        "written",
    }:
        raise ValueError
    if value["stage"] not in STAGES or value["activity"] not in {"network", "local"}:
        raise ValueError
    for key, limit in (("received", max_bytes + 16 * 1024**2), ("written", max_bytes)):
        if type(value[key]) is not int or not 0 <= value[key] <= limit:
            raise ValueError
    instant = value["network_at"]
    if type(instant) not in (int, float) or not math.isfinite(instant) or instant < started:
        raise ValueError
    return value


class ChildOutput:
    def __init__(self, process, payload, max_bytes, started):
        self.process, self.payload = process, payload
        self.max_bytes, self.started = max_bytes, started
        self.lock = threading.Lock()
        self.progress = self.result = None
        self.done = False
        self.thread = threading.Thread(target=self._read, daemon=True, name="model-acquire-output")

    def snapshot(self):
        with self.lock:
            return self.progress, self.result, self.done

    def _read(self):
        try:
            self.process.stdin.write(self.payload)
            self.process.stdin.close()
            while raw := self.process.stdout.readline(FRAME_LIMIT + 1):
                if len(raw) > FRAME_LIMIT or not raw.endswith(b"\n"):
                    raise ValueError
                value = strict_json_loads(raw.decode("utf-8"))
                if not isinstance(value, dict):
                    raise ValueError
                with self.lock:
                    if self.result is not None:
                        raise ValueError
                    if set(value) == {"progress"}:
                        current = progress_frame(value["progress"], self.max_bytes, self.started)
                        if self.progress is not None:
                            previous = self.progress
                            if any(
                                current[k] < previous[k]
                                for k in ("received", "written", "network_at")
                            ):
                                raise ValueError
                            if all(
                                current[k] == previous[k]
                                for k in ("received", "written", "stage", "activity")
                            ):
                                # A repeated notification is not evidence of network IO.
                                current["network_at"] = previous["network_at"]
                        self.progress = current
                    else:
                        self.result = value
        except Exception:
            with self.lock:
                self.result = {"protocol_error": True}
        finally:
            for stream in (self.process.stdin, self.process.stdout):
                if stream is not None:
                    try:
                        stream.close()
                    except OSError, ValueError:
                        pass
            with self.lock:
                self.done = True


def wait_tick(seconds):
    threading.Event().wait(seconds)
