"""One direct child owns all acquisition IO; the caller enforces cancellation and time."""

import json
import math
import subprocess
import sys
import time
from dataclasses import asdict, dataclass
from pathlib import Path

from inferyard.config.preparation_io import PreparationError
from inferyard.platforms.model_source import parse_source, validate_token_env

REASONS = {
    "invalid_model_source": 2,
    "model_source_metadata_mismatch": 2,
    "model_acquire_incomplete": 2,
    "output_exists": 2,
    "io_error": 4,
    "cancelled": 130,
}


@dataclass(frozen=True, slots=True)
class Budget:
    total_seconds: float = 1800
    read_seconds: float = 180
    cleanup_seconds: float = 60
    max_bytes: int = 20 * 1024**3

    def __post_init__(self):
        for value, limit in (
            (self.total_seconds, 1800),
            (self.read_seconds, 180),
            (self.cleanup_seconds, 60),
        ):
            if (
                type(value) not in (int, float)
                or not math.isfinite(value)
                or not 0 < value <= limit
            ):
                raise PreparationError("invalid_model_source")
        if type(self.max_bytes) is not int or not 0 < self.max_bytes <= 20 * 1024**3:
            raise PreparationError("invalid_model_source")


def _stop(process, budget):
    deadline = time.monotonic() + budget.cleanup_seconds
    try:
        process.terminate()
    except ProcessLookupError:
        pass
    try:
        process.wait(timeout=min(1, budget.cleanup_seconds / 2))
    except subprocess.TimeoutExpired:
        process.kill()
        try:
            process.wait(timeout=max(0, deadline - time.monotonic()))
        except subprocess.TimeoutExpired:
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
    deadline = time.monotonic() + budget.total_seconds
    process = subprocess.Popen(
        [sys.executable, "-m", "inferyard.platforms.model_source_worker"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        close_fds=True,
    )
    stop_requested = False
    try:
        try:
            raw, _ = process.communicate(payload, timeout=max(0, deadline - time.monotonic()))
        except subprocess.TimeoutExpired:
            stop_requested = True
            _stop(process, budget)
            raise PreparationError("model_acquire_incomplete") from None
        except KeyboardInterrupt:
            stop_requested = True
            _stop(process, budget)
            raise PreparationError("cancelled", 130) from None
        if process.returncode != 0:
            raise PreparationError("model_acquire_incomplete")
        try:
            result = json.loads(raw)
            if "reason" in result:
                reason = result["reason"]
                raise PreparationError(reason, REASONS[reason])
            return result["details"]
        except ValueError, KeyError, TypeError:
            raise PreparationError("model_acquire_incomplete") from None
    finally:
        if process.poll() is None and not stop_requested:
            _stop(process, budget)
        for stream in (process.stdin, process.stdout):
            if stream is not None:
                stream.close()
