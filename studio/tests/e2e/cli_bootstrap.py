"""Copied to the test's host directory as sitecustomize.py by NewEnv."""

from pathlib import Path

try:
    import inferyard.runtime.lock as locking

    root = Path(__file__).resolve().parent
    locking.LOCK_PATH = root / "host.lock"
    locking.STATE_PATH = root / "host-state.json"
except Exception:
    # site normally reports import errors and continues; never run unisolated.
    raise SystemExit("Studio E2E could not isolate CLI host files") from None
