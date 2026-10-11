"""One direct child owns all acquisition IO; the caller enforces cancellation and time."""

import json
import subprocess
import sys
import time
from dataclasses import asdict
from pathlib import Path

from inferyard.config.preparation_io import PreparationError
from inferyard.platforms.model_source import parse_source, validate_token_env
from inferyard.platforms.model_source_budget import Budget
from inferyard.platforms.model_source_ipc import ChildOutput, result_details, wait_tick
from inferyard.platforms.model_source_progress import (
    AcquisitionError,
    diagnostics,
    validate_diagnostics,
)

REASONS = {
    "invalid_model_source": 2,
    "model_source_metadata_mismatch": 2,
    "model_acquire_incomplete": 2,
    "output_exists": 2,
    "io_error": 4,
    "cancelled": 130,
}


def _stop(process, budget):
    deadline = time.monotonic() + budget.cleanup_seconds
    try:
        process.terminate()
    except ProcessLookupError:
        pass
    try:
        process.wait(timeout=min(1, budget.cleanup_seconds / 2))
    except subprocess.TimeoutExpired:
        try:
            process.kill()
        except ProcessLookupError:
            pass
        while True:
            remaining = max(0, deadline - time.monotonic())
            try:
                process.wait(timeout=min(1, remaining))
                break
            except subprocess.TimeoutExpired:
                if time.monotonic() >= deadline:
                    raise PreparationError("io_error", 4) from None


def acquire(url, out, token_env=None, *, budget=None):
    source = parse_source(url)
    validate_token_env(token_env)
    budget = Budget() if budget is None else budget
    payload = json.dumps(
        {
            "source": asdict(source),
            "out": str(Path(out).absolute()),
            "token_env": token_env,
            "budget": asdict(budget),
        }
    ).encode()
    if len(payload) > 32 * 1024:
        raise PreparationError("invalid_model_source")
    started = time.monotonic()
    deadline = started + budget.total_seconds
    process = subprocess.Popen(
        [sys.executable, "-m", "inferyard.platforms.model_source_worker"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        close_fds=True,
    )
    reader = ChildOutput(process, payload, budget.max_bytes, started)
    progress = None

    def failure(exception_class, reason="model_acquire_incomplete", code=2):
        return AcquisitionError(
            reason,
            code,
            diagnostics=diagnostics(
                progress["stage"] if progress else "preflight",
                exception_class,
                progress["written"] if progress else None,
                max(0.0, time.monotonic() - started),
            ),
        )

    try:
        reader.thread.start()
        try:
            while True:
                progress, result, done = reader.snapshot()
                now = time.monotonic()
                if now >= deadline:
                    raise failure("total_timeout")
                if progress is None or progress["activity"] == "network":
                    network_at = progress["network_at"] if progress else started
                    if network_at > now:
                        raise failure("protocol_error")
                    if now - network_at >= budget.read_seconds:
                        raise failure("read_timeout")
                if result is not None and "protocol_error" in result:
                    raise failure("protocol_error")
                if done and process.poll() is not None:
                    break
                wait_tick(min(0.02, budget.read_seconds / 4, max(0, deadline - now)))
        except KeyboardInterrupt:
            raise failure("cancelled", "cancelled", 130) from None
        if process.returncode != 0:
            raise failure("worker_exit")
        try:
            if not isinstance(result, dict):
                raise ValueError
            if "reason" in result:
                reason = result["reason"]
                detail = validate_diagnostics(result["diagnostics"], budget.max_bytes)
                raise AcquisitionError(reason, REASONS[reason], diagnostics=detail)
            return result_details(
                result["details"], budget.max_bytes, source.path.rsplit("/", 1)[-1]
            )
        except ValueError, KeyError, TypeError:
            raise failure("protocol_error") from None
    finally:
        cleanup_deadline = time.monotonic() + budget.cleanup_seconds
        if process.poll() is None:
            try:
                _stop(process, budget)
            except PreparationError:
                raise failure("cleanup_timeout", "io_error", 4) from None
        if reader.thread.ident is not None:
            while reader.thread.is_alive() and time.monotonic() < cleanup_deadline:
                reader.thread.join(timeout=min(1, max(0, cleanup_deadline - time.monotonic())))
            if reader.thread.is_alive():
                raise failure("cleanup_timeout", "io_error", 4)
