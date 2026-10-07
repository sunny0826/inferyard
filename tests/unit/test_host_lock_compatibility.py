"""Real kernel locks and fixed-old-baseline retirement, only in temporary roots."""

import os
import subprocess
from types import SimpleNamespace

import pytest

import inferyard.runtime.lock as locking
from inferyard.evidence.storage import atomic_bytes, json_bytes, read_json
from inferyard.platforms.identity import PreflightError
from inferyard.platforms.windows_host_paths import common_data_root
from inferyard.runtime import host_migration
from inferyard.runtime.host_receipt import receipt_path
from tests.helpers import python_worker, readline_timeout
from tests.host_state_helpers import old_lock


@pytest.fixture
def paths(tmp_path, monkeypatch):
    common, legacy = tmp_path / "ProgramData", tmp_path / "D"
    common.mkdir()
    monkeypatch.setattr(locking, "LOCK_PATH", common / "inferyard-host.lock")
    monkeypatch.setattr(locking, "STATE_PATH", common / "inferyard-host.state.json")
    monkeypatch.setattr(locking, "LEGACY_ROOT", legacy)
    return legacy, common


def clean(root, value=None):
    atomic_bytes(
        root / "local-ai-benchmark-host.state.json",
        json_bytes(value or {"schema_version": 1, "dirty": False}),
    )


def test_system_folder_comes_from_native_common_appdata_not_user_environment(monkeypatch):
    seen = []

    def query(window, folder, token, flags, result):
        seen.append((window, folder, token, flags))
        result.value = "C:/ProgramData"
        return 0

    class Query:
        def __call__(self, *args):
            return query(*args)

    monkeypatch.setenv("ProgramData", "Z:/per-user")
    monkeypatch.setattr(
        "inferyard.platforms.windows_host_paths.ctypes.WinDLL",
        lambda *args, **kwargs: SimpleNamespace(SHGetFolderPathW=Query()),
        raising=False,
    )
    assert str(common_data_root()).replace("\\", "/") == "C:/ProgramData"
    assert seen == [(None, 0x0023, None, 0)]


def test_new_check_lock_only_does_not_poison_fresh_initialization(paths):
    legacy, common = paths
    with pytest.raises(PreflightError, match="initialization_required"):
        locking.HostLock().__enter__()
    assert locking.LOCK_PATH.exists() and not locking.STATE_PATH.exists()
    assert host_migration.migrate()["mode"] == "fresh"
    inode = (common / "local-ai-benchmark-host.lock").stat().st_ino
    with pytest.raises(PreflightError, match="invalid_host_state"):
        old_lock(common).__enter__()
    assert (common / "local-ai-benchmark-host.lock").stat().st_ino == inode
    assert not legacy.exists()
    with locking.HostLock() as lock:
        assert not lock.state["dirty"]


@pytest.mark.parametrize("d_exists", [False, True])
def test_original_bytes_receipt_retirement_order_and_inode(paths, monkeypatch, d_exists):
    legacy, common = paths
    roots = [common]
    if d_exists:
        legacy.mkdir()
        roots.append(legacy)
    originals, inodes = {}, {}
    for root in roots:
        clean(root)
        with old_lock(root):
            pass
        originals[str(root)] = (root / "local-ai-benchmark-host.state.json").read_bytes()
        inodes[root] = (root / "local-ai-benchmark-host.lock").stat().st_ino
    stages = []
    monkeypatch.setattr(host_migration, "_checkpoint", stages.append)
    result = host_migration.migrate()
    assert result["ready_to_run"] is False and result["mode"] == "migrate"
    assert stages == [
        "locks",
        "receipt",
        "pending",
        "retired_common",
        *(["retired_d"] if d_exists else []),
        "ready",
    ]
    from inferyard.runtime.host_receipt import original

    receipt = read_json(receipt_path())
    assert {s["root"]: original(s) for s in receipt["slots"]} == originals
    for root in roots:
        assert (root / "local-ai-benchmark-host.lock").stat().st_ino == inodes[root]
    with pytest.raises(PreflightError, match="invalid_host_state"):
        old_lock(common, legacy).__enter__()


def test_ready_normal_use_ignores_old_locks_and_later_d(paths):
    legacy, common = paths
    host_migration.migrate()
    legacy.mkdir()
    clean(legacy)
    old = old_lock(common, legacy)
    with pytest.raises(PreflightError, match="invalid_host_state"):
        old.__enter__()
    (legacy / "local-ai-benchmark-host.state.json").write_bytes(b"broken")
    fd = locking.open_nofollow(common / "local-ai-benchmark-host.lock", os.O_RDWR)
    try:
        locking._lock(fd)
        with locking.HostLock() as new:
            new.dirty(
                "run",
                {"url": "http://127.0.0.1:1", "server_pid": 111, "process_start_ticks": 222},
                "request",
            )
            assert new.state["migration"]["phase"] == "ready"
    finally:
        os.close(fd)
    raw = locking.STATE_PATH.read_bytes()
    with pytest.raises(PreflightError):
        host_migration.migrate()
    assert locking.STATE_PATH.read_bytes() == raw


@pytest.mark.parametrize(
    "damage",
    ["lock_only", "dirty", "bad_json", "bool_version", "symlink", "hardlink", "permissions"],
)
def test_old_state_fail_closed_preserves_bytes(paths, damage):
    legacy, common = paths
    legacy.mkdir()
    clean(common)
    path = legacy / "local-ai-benchmark-host.state.json"
    if damage == "lock_only":
        (legacy / "local-ai-benchmark-host.lock").touch(mode=0o600)
    elif damage == "symlink":
        from tests.helpers import symlink_or_skip

        symlink_or_skip(path, common / "local-ai-benchmark-host.state.json")
    elif damage == "hardlink":
        os.link(common / "local-ai-benchmark-host.state.json", path)
    else:
        value = {
            "schema_version": True if damage == "bool_version" else 1,
            "dirty": damage == "dirty",
        }
        clean(legacy, value)
        if damage == "bad_json":
            path.write_bytes(b"bad")
        if damage == "permissions":
            if os.name == "nt":
                pytest.skip("POSIX mode case; Windows native ACL verification remains separate")
            path.chmod(0o644)
    before = path.read_bytes() if path.exists() else None
    with pytest.raises(PreflightError):
        host_migration.migrate()
    assert (path.read_bytes() if path.exists() else None) == before
    assert not receipt_path().exists() and not locking.STATE_PATH.exists()
    for root in (common, legacy):
        fd = locking.open_nofollow(root / "local-ai-benchmark-host.lock", os.O_RDWR)
        try:
            locking._lock(fd)
        finally:
            os.close(fd)


@pytest.mark.parametrize(
    "stage", ["locks", "receipt", "pending", "retired_common", "retired_d", "ready"]
)
def test_process_kill_retry_same_receipt_without_reset(paths, stage):
    legacy, common = paths
    legacy.mkdir()
    clean(legacy)
    clean(common)
    script = """
import sys, time
from pathlib import Path
import inferyard.runtime.lock as lock
from inferyard.runtime import host_migration as migration
lock.LOCK_PATH = Path(sys.argv[1]) / 'inferyard-host.lock'
lock.STATE_PATH = Path(sys.argv[1]) / 'inferyard-host.state.json'
lock.LEGACY_ROOT = Path(sys.argv[2])
def checkpoint(stage):
    if stage == sys.argv[3]:
        print('paused', flush=True)
        time.sleep(30)
migration._checkpoint = checkpoint
migration.migrate()
"""
    child = subprocess.Popen(
        python_worker(script, str(common), str(legacy), stage), stdout=subprocess.PIPE, text=True
    )
    try:
        assert readline_timeout(child) == "paused"
        with pytest.raises(PreflightError, match="host_lock_unavailable"):
            host_migration.migrate()
        raw = receipt_path().read_bytes() if receipt_path().exists() else None
        child.kill()
        child.wait(timeout=8)
        if stage != "ready":
            with pytest.raises(PreflightError):
                locking.HostLock().__enter__()
        if stage in ("retired_common", "retired_d", "ready"):
            with pytest.raises(PreflightError, match="invalid_host_state"):
                old_lock(common, legacy).__enter__()
        assert host_migration.migrate()["status"] in ("migrated", "already_migrated")
        if raw is not None:
            assert receipt_path().read_bytes() == raw
        with locking.HostLock() as ready:
            assert ready.state["dirty"] is False
    finally:
        if child.poll() is None:
            child.kill()
            child.wait(timeout=8)
        child.stdout.close()


def test_old_state_change_after_receipt_requires_investigation(paths, monkeypatch):
    _, common = paths
    clean(common)

    def crash(stage):
        if stage == "receipt":
            raise KeyboardInterrupt

    monkeypatch.setattr(host_migration, "_checkpoint", crash)
    with pytest.raises(KeyboardInterrupt):
        host_migration.migrate()
    receipt = receipt_path().read_bytes()
    path = common / "local-ai-benchmark-host.state.json"
    path.write_bytes(b'{"dirty":false, "schema_version":1}\n')
    monkeypatch.setattr(host_migration, "_checkpoint", lambda stage: None)
    with pytest.raises(PreflightError, match="source_changed_investigate"):
        host_migration.migrate()
    assert receipt_path().read_bytes() == receipt and not locking.STATE_PATH.exists()


def test_ready_reentry_cannot_clear_dirty_and_receipt_loss_cannot_reinitialize(paths):
    host_migration.migrate()
    with locking.HostLock() as lock:
        lock.dirty(
            "run",
            {"url": "http://127.0.0.1:1", "server_pid": 111, "process_start_ticks": 222},
            "request",
        )
    raw = locking.STATE_PATH.read_bytes()
    with pytest.raises(PreflightError, match="new_dirty_use_recovery"):
        host_migration.migrate()
    assert locking.STATE_PATH.read_bytes() == raw
    receipt_path().unlink()  # Deliberate damage to this test's temporary evidence.
    with pytest.raises(PreflightError):
        locking.HostLock().__enter__()
    with pytest.raises(PreflightError):
        host_migration.migrate()
    assert locking.STATE_PATH.read_bytes() == raw


def test_old_running_process_excludes_migration(paths):
    _, common = paths
    script = """
import sys, time
from pathlib import Path
from tests.host_state_helpers import old_lock
with old_lock(Path(sys.argv[1])):
    print('held', flush=True)
    time.sleep(30)
"""
    child = subprocess.Popen(python_worker(script, str(common)), stdout=subprocess.PIPE, text=True)
    try:
        assert readline_timeout(child) == "held"
        with pytest.raises(PreflightError, match="host_lock_unavailable"):
            host_migration.migrate()
        assert not receipt_path().exists()
    finally:
        child.kill()
        child.wait(timeout=8)
        child.stdout.close()


@pytest.mark.parametrize(
    "damage", ["extra_slot", "identity_type", "fresh_with_original", "state_identity_null"]
)
def test_receipt_shape_damage_fails_closed(paths, damage):
    _, common = paths
    clean(common)
    host_migration.migrate()
    before = locking.STATE_PATH.read_bytes()
    value = read_json(receipt_path())
    if damage == "extra_slot":
        value["slots"].insert(0, 42)
    elif damage == "identity_type":
        value["slots"][0]["lock_identity"]["inode"] = True
    elif damage == "fresh_with_original":
        value["mode"] = "fresh"
    else:
        value["slots"][0]["state_identity"] = None
    atomic_bytes(receipt_path(), json_bytes(value), overwrite=True)
    for action in (lambda: locking.HostLock().__enter__(), host_migration.migrate):
        with pytest.raises(PreflightError):
            action()
        assert locking.STATE_PATH.read_bytes() == before


def test_racing_old_lock_creation_is_not_recorded_as_fresh(paths, monkeypatch):
    _, common = paths
    path = common / "local-ai-benchmark-host.lock"
    actual_open = locking.open_nofollow
    raced = False

    def race(target, flags, *args):
        nonlocal raced
        if target == path and flags & os.O_EXCL and not raced:
            raced = True
            fd = actual_open(target, flags, *args)
            os.close(fd)  # The old process crashed before writing its state.
        return actual_open(target, flags, *args)

    monkeypatch.setattr(locking, "open_nofollow", race)
    with pytest.raises(PreflightError, match="old_lock_only_investigate"):
        host_migration.migrate()
    assert raced and not receipt_path().exists() and not locking.STATE_PATH.exists()
