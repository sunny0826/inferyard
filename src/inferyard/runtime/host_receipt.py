"""Immutable migration proof; normal runs inspect only the new root and receipt."""

import base64
import binascii
import re

from inferyard.runtime.host_files import decode, identity, read_bytes, require, root_identity, sha

DEFINITION = "host-state-migration.v1"
OLD_SHA = "4bfa0a676b5c03a6e348cea180501d1e6a0df33dea260c69cc52807a05d138a1"
NEW_SHA = "3c969665902d636fb14b994e03601b7cd6e11ad373f987b066c109dba9d739ae"


def receipt_path():
    from inferyard.runtime import lock

    return lock.STATE_PATH.parent / "inferyard-host-migration.json"


def envelope(receipt, digest, phase):
    return {
        "definition": DEFINITION,
        "id": receipt["migration_id"],
        "receipt_sha256": digest,
        "phase": phase,
    }


def retired(receipt, digest):
    return {
        "schema_version": 2,
        "definition": "inferyard-host-retired.v1",
        "migration_id": receipt["migration_id"],
        "receipt_sha256": digest,
    }


def load_receipt():
    from inferyard.runtime import lock

    raw = read_bytes(receipt_path())
    require(raw is not None, "host_state_receipt_missing")
    value = decode(raw)
    require(
        set(value)
        == {
            "definition",
            "migration_id",
            "old_baseline_sha256",
            "new_baseline_sha256",
            "mode",
            "new",
            "slots",
        },
        "host_state_receipt_invalid",
    )
    require(
        value["definition"] == DEFINITION
        and value["old_baseline_sha256"] == OLD_SHA
        and value["new_baseline_sha256"] == NEW_SHA,
        "host_state_receipt_invalid",
    )
    require(
        type(value["migration_id"]) is str and re.fullmatch(r"[0-9a-f]{32}", value["migration_id"]),
        "host_state_receipt_invalid",
    )
    require(value["mode"] in ("fresh", "migrate"), "host_state_receipt_invalid")
    require(
        value["new"]
        == {
            "root": str(lock.STATE_PATH.parent),
            "root_identity": root_identity(lock.STATE_PATH.parent),
            "lock_identity": identity(lock.LOCK_PATH.lstat()),
        },
        "host_state_identity_changed",
    )
    slots = value["slots"]
    require(type(slots) is list and len(slots) in (1, 2), "host_state_receipt_invalid")
    allowed = [str(lock.STATE_PATH.parent)]
    if lock.LEGACY_ROOT is not None:
        allowed.insert(0, str(lock.LEGACY_ROOT))
    require(
        [s.get("root") for s in slots if type(s) is dict] in (allowed, allowed[-1:]),
        "host_state_receipt_invalid",
    )
    for slot in slots:
        require(type(slot) is dict, "host_state_receipt_invalid")
        require(
            set(slot)
            == {
                "root",
                "root_identity",
                "lock_identity",
                "lock_existed",
                "state_existed",
                "state_identity",
                "state_base64",
                "state_sha256",
                "state_bytes",
            },
            "host_state_receipt_invalid",
        )
        require(
            type(slot["lock_existed"]) is bool and type(slot["state_existed"]) is bool,
            "host_state_receipt_invalid",
        )
        require(
            (type(slot["state_identity"]) is dict) == slot["state_existed"],
            "host_state_receipt_invalid",
        )
        for key in ("root_identity", "lock_identity", "state_identity"):
            item = slot[key]
            if key == "state_identity" and not slot["state_existed"]:
                require(item is None, "host_state_receipt_invalid")
                continue
            require(
                type(item) is dict
                and set(item) == {"device", "inode"}
                and all(type(v) is int and v >= 0 for v in item.values()),
                "host_state_receipt_invalid",
            )
        raw_original = original(slot)
        if raw_original is not None:
            from inferyard.runtime.host_files import validate_state

            state = validate_state(decode(raw_original))
            require(not state["dirty"] and "migration" not in state, "host_state_receipt_invalid")
        require(
            not (slot["lock_existed"] and not slot["state_existed"]), "host_state_receipt_invalid"
        )
    require(
        value["mode"]
        == ("migrate" if any(s["state_existed"] or s["lock_existed"] for s in slots) else "fresh"),
        "host_state_receipt_invalid",
    )
    return value, sha(raw)


def original(slot):
    if not slot["state_existed"]:
        require(
            all(slot[k] is None for k in ("state_base64", "state_sha256", "state_bytes")),
            "host_state_receipt_invalid",
        )
        return None
    try:
        raw = base64.b64decode(slot["state_base64"], validate=True)
    except (TypeError, ValueError, binascii.Error) as exc:
        from inferyard.platforms.identity import PreflightError

        raise PreflightError("host_state_receipt_invalid") from exc
    require(
        type(slot["state_bytes"]) is int
        and slot["state_bytes"] == len(raw)
        and slot["state_sha256"] == sha(raw),
        "host_state_receipt_invalid",
    )
    return raw


def require_ready(state):
    require(type(state.get("migration")) is dict, "host_state_migration_required")
    receipt, digest = load_receipt()
    require(
        state["migration"] == envelope(receipt, digest, "ready"), "host_state_migration_pending"
    )
    return state
