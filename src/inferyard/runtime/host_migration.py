"""One-time explicit retirement, under every old lock and the new host lock."""

import base64
import sys
import uuid
from pathlib import Path

from inferyard.evidence.storage import json_bytes
from inferyard.platforms.identity import PreflightError
from inferyard.runtime.host_files import (
    decode,
    exists,
    identity,
    publish_state,
    read_bytes,
    require,
    root_identity,
    sha,
    validate_state,
)
from inferyard.runtime.host_receipt import (
    DEFINITION,
    NEW_SHA,
    OLD_SHA,
    envelope,
    load_receipt,
    original,
    receipt_path,
    retired,
)


def old_pairs():
    from inferyard.runtime import lock

    roots = [lock.STATE_PATH.parent]
    if lock.LEGACY_ROOT is not None and exists(lock.LEGACY_ROOT):
        roots.insert(0, lock.LEGACY_ROOT)
    return [
        (root / "local-ai-benchmark-host.lock", root / "local-ai-benchmark-host.state.json")
        for root in roots
    ]


def _checkpoint(stage):
    """Fault injection seam used only by isolated process tests."""


def _check_slots(receipt, digest):
    marker = json_bytes(retired(receipt, digest))
    for slot in receipt["slots"]:
        root = Path(slot["root"])
        require(
            root_identity(root) == slot["root_identity"]
            and identity((root / "local-ai-benchmark-host.lock").lstat()) == slot["lock_identity"],
            "host_state_identity_changed",
        )
        actual = read_bytes(root / "local-ai-benchmark-host.state.json")
        require(actual in (original(slot), marker), "host_state_source_changed_investigate")
        if actual is not None and actual != marker:
            require(
                identity((root / "local-ai-benchmark-host.state.json").lstat())
                == slot["state_identity"],
                "host_state_identity_changed",
            )


def _fresh_receipt(pairs, existed):
    from inferyard.runtime import lock

    require(not exists(lock.STATE_PATH), "host_state_without_receipt")
    slots = []
    for (lock_path, state_path), was_present in zip(pairs, existed, strict=True):
        raw = read_bytes(state_path)
        require(not (was_present and raw is None), "host_state_old_lock_only_investigate")
        if raw is not None:
            state = validate_state(decode(raw))
            require(not state["dirty"], "host_state_old_dirty_use_original_recovery")
            require("migration" not in state, "invalid_host_state")
        slots.append(
            {
                "root": str(state_path.parent),
                "root_identity": root_identity(state_path.parent),
                "lock_identity": identity(lock_path.lstat()),
                "lock_existed": was_present,
                "state_existed": raw is not None,
                "state_identity": identity(state_path.lstat()) if raw is not None else None,
                "state_base64": base64.b64encode(raw).decode() if raw is not None else None,
                "state_sha256": sha(raw) if raw is not None else None,
                "state_bytes": len(raw) if raw is not None else None,
            }
        )
    return {
        "definition": DEFINITION,
        "migration_id": uuid.uuid4().hex,
        "old_baseline_sha256": OLD_SHA,
        "new_baseline_sha256": NEW_SHA,
        "mode": "migrate"
        if any(s["state_existed"] or s["lock_existed"] for s in slots)
        else "fresh",
        "new": {
            "root": str(lock.STATE_PATH.parent),
            "root_identity": root_identity(lock.STATE_PATH.parent),
            "lock_identity": identity(lock.LOCK_PATH.lstat()),
        },
        "slots": slots,
    }


def migrate():
    from inferyard.runtime import lock

    held = lock.HostLock()
    try:
        pairs = old_pairs()
        roots = {str(path.parent): root_identity(path.parent) for path, _ in pairs}
        for path, _ in [*pairs, (lock.LOCK_PATH, lock.STATE_PATH)]:
            held._acquire(path)
        existed = [path not in held._created_paths for path, _ in pairs]
        _checkpoint("locks")
        if exists(receipt_path()):
            receipt, digest = load_receipt()
        else:
            receipt = _fresh_receipt(pairs, existed)
            held._check_locks()
            publish_state(receipt_path(), receipt, overwrite=False)
            digest = sha(read_bytes(receipt_path()))
        _checkpoint("receipt")
        _check_slots(receipt, digest)
        raw = read_bytes(lock.STATE_PATH)
        state = decode(raw) if raw is not None else None
        if state is not None and state.get("migration") == envelope(receipt, digest, "ready"):
            validate_state(state)
            require(not state["dirty"], "host_state_new_dirty_use_recovery")
            require(
                all(
                    read_bytes(Path(s["root"]) / "local-ai-benchmark-host.state.json")
                    == json_bytes(retired(receipt, digest))
                    for s in receipt["slots"]
                ),
                "host_state_retirement_changed",
            )
            return {"status": "already_migrated", "ready_to_run": False}
        pending = {"schema_version": 1, "migration": envelope(receipt, digest, "pending")}
        require(state is None or state == pending, "host_state_transaction_conflict")
        require(
            [str(p.parent) for p, _ in pairs] == [s["root"] for s in receipt["slots"]],
            "host_state_identity_changed",
        )
        if state is None:
            held._check_locks()
            publish_state(lock.STATE_PATH, pending, overwrite=False)
        _checkpoint("pending")
        # Common root first, D second: old baseline always reads the common state.
        for slot in reversed(receipt["slots"]):
            _check_slots(receipt, digest)
            path = Path(slot["root"]) / "local-ai-benchmark-host.state.json"
            marker = retired(receipt, digest)
            if read_bytes(path) != json_bytes(marker):
                held._check_locks()
                publish_state(path, marker, overwrite=slot["state_existed"])
            _checkpoint(
                "retired_common" if slot["root"] == str(lock.STATE_PATH.parent) else "retired_d"
            )
        _check_slots(receipt, digest)
        require(
            all(root_identity(Path(root)) == value for root, value in roots.items()),
            "host_state_identity_changed",
        )
        held._check_locks()
        require(load_receipt() == (receipt, digest), "host_state_receipt_changed")
        ready = {
            "schema_version": 1,
            "dirty": False,
            "migration": envelope(receipt, digest, "ready"),
        }
        publish_state(lock.STATE_PATH, ready, overwrite=True)
        _checkpoint("ready")
        return {"status": "migrated", "ready_to_run": False, "mode": receipt["mode"]}
    except OSError as exc:
        raise PreflightError("host_lock_unavailable") from exc
    finally:
        active = sys.exc_info()[1]
        error = held._release_fds()
        if error is not None:
            if active is not None:
                active.add_note("host_lock_cleanup_failed")
            else:
                raise error
