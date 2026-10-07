"""Isolated host lifecycle fixtures; never select the machine's fixed roots."""

import hashlib
from pathlib import Path
from types import ModuleType

import inferyard.runtime.lock as locking
from inferyard.runtime.host_receipt import OLD_SHA


def initialize():
    from inferyard.runtime.host_migration import migrate

    assert locking.STATE_PATH.parent != locking._HOST_ROOT
    if locking.STATE_PATH.exists():
        with locking.HostLock():
            return {"status": "existing_fixture"}
    return migrate()


def old_lock(common, legacy=None):
    raw = (Path(__file__).parent / "fixtures/host-lock-baseline.txt").read_bytes()
    assert hashlib.sha256(raw).hexdigest() == OLD_SHA
    module = ModuleType("fixed_old_host_lock")
    # Only normalize the import namespace; every old control-flow byte stays fixed.
    exec(
        compile(raw.replace(b"local_ai_bench", b"inferyard"), "fixed_old_host_lock", "exec"),
        module.__dict__,
    )
    module.LOCK_PATH = common / "local-ai-benchmark-host.lock"
    module.STATE_PATH = common / "local-ai-benchmark-host.state.json"
    module.LEGACY_ROOT = legacy
    return module.HostLock()
