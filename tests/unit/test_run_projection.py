"""Continuation fields match full reduction without replaying measurement logs."""

import json
from pathlib import Path

import pytest

from inferyard.evidence.ledger import read_run_projection, read_trial
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json, sha256_file
from tests.unit.test_ledger import attempt, finish, journal
from tests.unit.test_ledger import setup as ledger_setup

setup = ledger_setup
REQUEST_FIELDS = (
    "case_id",
    "execution_state",
    "category",
    "quality_state",
    "score",
    "t_send_ns",
    "t_terminal_ns",
)


def assert_projection(path):
    metadata = {}
    full = read_trial(path, metadata=metadata)
    projected = read_run_projection(path)
    for key in ("run", "plan", "config", "bundle", "selection", "events_sha256"):
        assert projected[key] == full[key], key
    assert projected["manifest_sealed"] == (metadata["manifest"] is not None)
    assert projected["service_drain"] == metadata["service_drain"]
    for key in ("stop_reason", "counts", "completeness", "scope_complete", "evidence_complete"):
        assert projected["summary"][key] == full["summary"][key], key
    assert projected["summary"].get("duration") == full["summary"].get("duration")
    assert len(projected["requests"]) == len(full["requests"])
    for actual, expected in zip(projected["requests"], full["requests"], strict=True):
        for key in REQUEST_FIELDS:
            assert (key in actual) == (key in expected), key
            assert actual.get(key) == expected.get(key), key
    return projected


def reseal_file(path, name):
    manifest = read_json(path / "manifest.json")
    manifest["files"][name]["sha256"] = sha256_file(path / name)
    manifest["files"][name]["bytes"] = (path / name).stat().st_size
    (path / "manifest.json").write_bytes(json_bytes(manifest))


def same_error(path):
    with pytest.raises(EvidenceError) as expected:
        read_trial(path)
    with pytest.raises(EvidenceError) as actual:
        read_run_projection(path)
    assert str(actual.value) == str(expected.value)
    return str(actual.value)


@pytest.mark.parametrize("sealed", [True, False])
@pytest.mark.parametrize("diagnostic", [True, False])
@pytest.mark.parametrize(
    "states",
    [[], ["completed"] * 5, ["completed", "failed", "cancelled", "invalid"]],
)
def test_fixed_states_and_completeness(tmp_path, setup, states, sealed, diagnostic):
    store = journal(tmp_path, setup, diagnostic=diagnostic)
    for index, state in enumerate(states):
        attempt(store, setup, index, state, finish="length")
    finish(store, "plan_finished" if len(states) == 5 else "tool_interrupted", seal=sealed)
    assert not (store.path / "summary.json").exists()
    assert_projection(store.path)


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
    result = assert_projection(store.path)
    assert result["summary"]["completeness"] == "incomplete"
    assert not result["summary"]["evidence_complete"]


def test_missing_score_and_saved_summary_are_reconstructed(tmp_path, setup):
    store = journal(tmp_path, setup)
    for index in range(5):
        attempt(store, setup, index, "completed", score=False)
    finish(store)
    # Derived summaries are neither required nor authoritative in read_trial.
    (store.path / "summary.json").write_bytes(b'{"forged": true}')
    result = assert_projection(store.path)
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
    result = assert_projection(store.path)
    assert bool(result["service_drain"]) == (sealed and idle)


@pytest.mark.parametrize("sealed", [True, False])
@pytest.mark.parametrize("repair_seal", [True, False])
@pytest.mark.parametrize("bad", [b"{oops}\n", b"[]\n", b'{"x":1,"x":2}\n', b'{"x":NaN}\n'])
def test_corrupt_json_error_matches_full_reader(tmp_path, setup, bad, sealed, repair_seal):
    store = journal(tmp_path, setup)
    finish(store, "tool_interrupted", seal=sealed)
    (store.path / "events.jsonl").write_bytes(bad)
    if sealed and repair_seal:
        reseal_file(store.path, "events.jsonl")
    same_error(store.path)


@pytest.mark.parametrize("sealed", [True, False])
@pytest.mark.parametrize(
    "name",
    [
        "run.json",
        "plan.json",
        "selection.json",
        "config.frozen.json",
        "bundle.json",
        "events.jsonl",
        "memory.jsonl",
    ],
)
def test_missing_original_error_matches_full_reader(tmp_path, setup, sealed, name):
    store = journal(tmp_path, setup)
    finish(store, "tool_interrupted", seal=sealed)
    (store.path / name).unlink()
    same_error(store.path)


@pytest.mark.parametrize(
    "event_kind", ["request_started", "request_finished", "score", "run_stopped"]
)
def test_consumed_events_keep_schema_validation(tmp_path, setup, event_kind):
    store = journal(tmp_path, setup)
    attempt(store, setup, 0, "completed")
    finish(store, "user_cancelled")
    path = store.path / "events.jsonl"
    records = [json.loads(line) for line in path.read_bytes().splitlines()]
    next(e for e in records if e["event_type"] == event_kind)["data"] = {}
    path.write_bytes(b"".join(json_bytes(e) for e in records))
    reseal_file(store.path, path.name)
    assert same_error(store.path) == "invalid_event"


def test_projection_does_not_read_sample_or_environment_timelines(tmp_path, setup, monkeypatch):
    from inferyard.evidence.trial_reads import TrialReads

    store = journal(tmp_path, setup)
    attempt(store, setup, 0, "completed")
    finish(store, "user_cancelled")
    expected = assert_projection(store.path)
    original_open = Path.open
    sample_reads = []

    class TailOnly:
        def __init__(self, stream):
            self.stream = stream

        def __enter__(self):
            return self

        def __exit__(self, *args):
            self.stream.close()

        def seek(self, *args):
            return self.stream.seek(*args)

        def read(self, size=-1):
            assert size == 1
            sample_reads.append(size)
            return self.stream.read(size)

    def guarded_open(path, *args, **kwargs):
        assert path.name not in ("environment.jsonl", "schedule.jsonl")
        stream = original_open(path, *args, **kwargs)
        return TailOnly(stream) if path.name == "memory.jsonl" else stream

    monkeypatch.setattr(Path, "open", guarded_open)
    monkeypatch.setattr(TrialReads, "jsonl", lambda *a: pytest.fail("full log read"))
    assert read_run_projection(store.path) == expected
    assert len(sample_reads) <= 1
