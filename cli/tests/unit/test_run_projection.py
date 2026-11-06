"""Partial evidence cannot be promoted when reading continuation metadata."""

import pytest

from inferyard.evidence.ledger import read_run_projection
from inferyard.evidence.storage import json_bytes, read_json, sha256_file
from tests.unit.test_ledger import attempt, finish, journal
from tests.unit.test_ledger import setup as ledger_setup

setup = ledger_setup


def reseal_file(path, name):
    manifest = read_json(path / "manifest.json")
    manifest["files"][name]["sha256"] = sha256_file(path / name)
    manifest["files"][name]["bytes"] = (path / name).stat().st_size
    (path / "manifest.json").write_bytes(json_bytes(manifest))


@pytest.mark.parametrize("tail", ["events.jsonl", "memory.jsonl"])
@pytest.mark.parametrize("sealed", [True, False])
def test_truncated_tail_never_promoted(tmp_path, setup, tail, sealed):
    store = journal(tmp_path, setup)
    for index in range(5):
        attempt(store, setup, index, "completed")
    finish(store, seal=sealed)
    with (store.path / tail).open("ab") as stream:
        stream.write(b'{"incomplete":')
    if sealed:
        reseal_file(store.path, tail)
    result = read_run_projection(store.path)
    assert result["summary"]["completeness"] == "incomplete"
    assert not result["summary"]["evidence_complete"]


def test_missing_score_and_saved_summary_are_reconstructed(tmp_path, setup):
    store = journal(tmp_path, setup)
    for index in range(5):
        attempt(store, setup, index, "completed", score=False)
    finish(store)
    (store.path / "summary.json").write_bytes(b'{"forged": true}')
    result = read_run_projection(store.path)
    assert all(r["quality_state"] == "unscorable" for r in result["requests"])


@pytest.mark.parametrize("sealed", [True, False])
@pytest.mark.parametrize("idle", [True, False])
def test_slots_drain_uses_last_request_and_seal(tmp_path, setup, sealed, idle):
    store = journal(tmp_path, setup)
    attempt(store, setup, 0, "completed")
    store.event(
        "idle_observed",
        "formal",
        f"{store.run_id}-r0",
        {"state": "idle" if idle else "busy", "source": "/slots:is_processing"},
        monotonic_ns=200,
    )
    finish(store, "user_cancelled", seal=sealed)
    result = read_run_projection(store.path)
    assert bool(result["service_drain"]) == (sealed and idle)
