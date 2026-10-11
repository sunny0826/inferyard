"""Credential-free acquisition state and failure diagnostics shared across the child boundary."""

import math
import ssl
import time
from dataclasses import dataclass, field

import httpx

from inferyard.config.preparation_io import PreparationError

STAGES = frozenset(
    {"preflight", "metadata", "redirect", "download-open", "download-body", "publish"}
)
CLASSES = frozenset(
    {
        "connect_timeout",
        "read_timeout",
        "write_timeout",
        "pool_timeout",
        "tls_error",
        "connect_error",
        "read_error",
        "write_error",
        "protocol_error",
        "http_error",
        "http_status",
        "redirect_rejected",
        "encoding_rejected",
        "metadata_mismatch",
        "invalid_source",
        "output_exists",
        "io_error",
        "size_limit",
        "preparation_error",
        "total_timeout",
        "cancelled",
        "worker_exit",
        "cleanup_timeout",
        "internal_error",
    }
)


class AcquisitionError(PreparationError):
    def __init__(self, reason, code=2, *, diagnostics):
        super().__init__(reason, code)
        self.diagnostics = diagnostics


def classification(exc):
    cause = exc
    for _ in range(4):
        if isinstance(cause, ssl.SSLError):
            return "tls_error"
        cause = cause.__cause__ if isinstance(cause, BaseException) else None
    for kind, name in (
        (httpx.ConnectTimeout, "connect_timeout"),
        (httpx.ReadTimeout, "read_timeout"),
        (httpx.WriteTimeout, "write_timeout"),
        (httpx.PoolTimeout, "pool_timeout"),
        (httpx.ConnectError, "connect_error"),
        (httpx.ReadError, "read_error"),
        (httpx.WriteError, "write_error"),
        (httpx.ProtocolError, "protocol_error"),
        (httpx.HTTPError, "http_error"),
        (OSError, "io_error"),
    ):
        if isinstance(exc, kind):
            return name
    if isinstance(exc, PreparationError):
        return {
            "model_source_metadata_mismatch": "metadata_mismatch",
            "invalid_model_source": "invalid_source",
            "output_exists": "output_exists",
            "io_error": "io_error",
            "cancelled": "cancelled",
        }.get(exc.reason, "preparation_error")
    return "internal_error"


def diagnostics(stage, exception_class, written, elapsed):
    return {
        "stage": stage if stage in STAGES else "preflight",
        "exception_class": exception_class if exception_class in CLASSES else "internal_error",
        "bytes_written": written,
        "elapsed_seconds": elapsed,
        "average_bytes_per_second": written / elapsed
        if written is not None and elapsed > 0
        else None,
        "progress_reason": "worker_progress_unavailable" if written is None else None,
    }


def validate_diagnostics(value, max_bytes):
    if not isinstance(value, dict) or set(value) != {
        "stage",
        "exception_class",
        "bytes_written",
        "elapsed_seconds",
        "average_bytes_per_second",
        "progress_reason",
    }:
        raise ValueError
    if value["stage"] not in STAGES or value["exception_class"] not in CLASSES:
        raise ValueError
    if value["bytes_written"] is None:
        if (
            value["progress_reason"] != "worker_progress_unavailable"
            or value["average_bytes_per_second"] is not None
        ):
            raise ValueError
    elif (
        type(value["bytes_written"]) is not int
        or not 0 <= value["bytes_written"] <= max_bytes
        or value["progress_reason"] is not None
    ):
        raise ValueError
    keys = (
        ("elapsed_seconds",)
        if value["bytes_written"] is None
        else ("elapsed_seconds", "average_bytes_per_second")
    )
    for key in keys:
        if type(value[key]) not in (int, float) or not math.isfinite(value[key]) or value[key] < 0:
            raise ValueError
    return dict(value)


@dataclass
class Progress:
    emit: object = None
    started: float = field(default_factory=time.monotonic)
    stage: str = "preflight"
    activity: str = "local"
    network_at: float = field(default_factory=time.monotonic)
    received: int = 0
    written: int = 0

    def update(self, stage, activity, *, received=0, written=0):
        # Only completed IO or entry into a network operation changes its clock.
        if received or (activity == "network" and self.activity != "network"):
            self.network_at = time.monotonic()
        self.stage, self.activity = stage, activity
        self.received += received
        self.written += written
        if self.emit is not None:
            self.emit({"progress": self.snapshot()})

    def snapshot(self):
        return {
            "stage": self.stage,
            "activity": self.activity,
            "network_at": self.network_at,
            "received": self.received,
            "written": self.written,
        }

    def failure(self, exc=None, *, reason=None, exception_class=None, code=None):
        if isinstance(exc, AcquisitionError):
            return exc
        if reason is None:
            reason = (
                exc.reason
                if isinstance(exc, PreparationError)
                else ("io_error" if isinstance(exc, OSError) else "model_acquire_incomplete")
            )
        if code is None:
            code = (
                exc.code if isinstance(exc, PreparationError) else 4 if reason == "io_error" else 2
            )
        return AcquisitionError(
            reason,
            code,
            diagnostics=diagnostics(
                self.stage,
                exception_class or classification(exc),
                self.written,
                max(0.0, time.monotonic() - self.started),
            ),
        )
