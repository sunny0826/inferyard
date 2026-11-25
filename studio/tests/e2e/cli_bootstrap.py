"""Copied to the test's host directory as sitecustomize.py by NewEnv."""

import importlib.util
from pathlib import Path

if importlib.util.find_spec("inferyard") is not None:
    try:
        import inferyard.runtime.lock as locking

        root = Path(__file__).resolve().parent
        locking.LOCK_PATH = root / "host.lock"
        locking.STATE_PATH = root / "host-state.json"
    except Exception:
        raise SystemExit("Studio E2E could not isolate CLI host files") from None
