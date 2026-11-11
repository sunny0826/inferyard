"""Real temporary-file checks for the production layout 2 durability boundary."""

import hashlib
import json

import pytest

from inferyard.adapters.requests import request_body
from inferyard.analysis.scoring import ScoringContext
from inferyard.config.loader import load_config
from inferyard.config.single_plan import compile_single_plan
from inferyard.errors import EvidenceError
from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.request_snapshots import messages_sha256
from inferyard.evidence.storage import EvidenceStore, Redactor, read_json, read_jsonl
from inferyard.evidence.trial_evidence import TrialEvidence


@pytest.fixture
def make_store(tmp_path, config_path):
    loaded = load_config(config_path)
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    plan = compile_single_plan(config, bundle)
    stores = []

    def make(**kwargs):
        kwargs.setdefault("scoring_context", ScoringContext())
        store = TrialJournal(
            tmp_path, plan, plan["trials"][0]["trial_id"], config, bundle, **kwargs
        )
        stores.append(store)
        return store

    yield make, config, bundle
    for store in stores:
        store.close()


def event_body(config, body):
    return {
        "model": body["model"],
        "stream": body["stream"],
        "generation": config["generation"],
        "messages_sha256": messages_sha256(body["messages"]),
    }


def start(store, config, bundle, body, *, key="prepared-1", phase="formal"):
    return store.event(
        "request_started",
        phase,
        key,
        {
            "case_id": bundle["cases"][0]["case_id"] if phase == "formal" else None,
            "plan_index": 0 if phase == "formal" else None,
            "attempt": 1,
            "body": event_body(config, body),
        },
    )


@pytest.mark.parametrize("opened", [0, 3, 5])
def test_interrupted_log_creation_reads_only_real_partial_evidence(
    make_store, tmp_path, monkeypatch, capsys, opened
):
    from inferyard.cli import main
    from inferyard.analysis.ledger import read_trial

    make, _, bundle = make_store
    open_logs = EvidenceStore._open_logs

    def interrupt(store, names):
        open_logs(store, names[:opened])
        store.close()
        raise EvidenceError("fixture_log_creation_interrupted")

    monkeypatch.setattr(EvidenceStore, "_open_logs", interrupt)
    with pytest.raises(EvidenceError, match="fixture_log_creation_interrupted"):
        make()
    root = next(tmp_path.iterdir())
    before = {path.name: path.read_bytes() for path in root.iterdir()}
    data = read_trial(root)
    summary = data["summary"]
    assert summary["counts"]["planned"] == len(bundle["cases"])
    assert summary["counts"]["not_executed"] == len(bundle["cases"])
    assert summary["counts"]["executed"] == 0
    assert not summary["evidence_complete"] and not summary["scope_complete"]
    assert "manifest_missing_unsealed_run" in summary["limitations"]
    if "events.jsonl" not in before:
        assert data["events_sha256"] is None
    else:
        assert data["events_sha256"] == hashlib.sha256(before["events.jsonl"]).hexdigest()
    assert all(
        ref["path"] in before
        for metric in summary["metric_observations"]
        for ref in metric["evidence_refs"]
    )
    assert main(["verify", "--path", str(root)]) == 3
    result = json.loads(capsys.readouterr().out)
    assert result["status"] == "partial" and result["details"]["integrity"] == "unsealed"
    assert before == {path.name: path.read_bytes() for path in root.iterdir()}


@pytest.mark.parametrize("sealed", [False, True])
def test_missing_prepared_file_never_authorizes_started_requests(make_store, sealed):
    from inferyard.analysis.ledger import read_trial

    make, config, bundle = make_store
    store = make()
    body = request_body(config, bundle["cases"][0]["prompt"])
    store.prepare_request("prepared-1", "formal", 1, body)
    start(store, config, bundle, body)
    if sealed:
        store.seal()
    store.close()
    (store.path / "request-bodies.jsonl").unlink()
    reason = "invalid_prepared_request" if sealed else "request_snapshot_missing"
    with pytest.raises(EvidenceError, match=reason):
        read_trial(store.path)


def test_configuration_source_binds_redacted_raw_utf8_and_crlf(make_store):
    make, _, _ = make_store
    raw = '# 原始\r\ncredential = "private-credential"\r\n'.encode()
    expected = raw.replace(b"private-credential", b"[REDACTED]")
    store = make(
        redactor=Redactor(["private-credential"]),
        configuration_source={
            "media_type": "application/toml",
            "data": raw,
            "missing_reason": None,
        },
    )
    evidence = TrialEvidence(store.path)
    assert evidence.configuration_source() == {
        "media_type": "application/toml",
        "data": expected,
        "missing_reason": None,
    }
    source = read_json(store.path / "inputs.json")["configuration_source"]
    assert source["bytes"] == len(expected)
    assert source["sha256"] == hashlib.sha256(expected).hexdigest()
    assert not (store.path / "config.input.toml").exists()


def test_source_missing_fact_is_explicit_and_never_reconstructed(make_store):
    make, _, _ = make_store
    store = make()
    assert TrialEvidence(store.path).configuration_source() == {
        "media_type": None,
        "data": None,
        "missing_reason": "not_persisted",
    }


def test_observation_authority_and_closed_queries_follow_durable_publication(make_store):
    make, _, _ = make_store
    store = make()
    assert not store.has_observation("identity")
    store.observe("identity", {"verification": "fixture"}, stage="post_probe")
    assert store.has_observation("identity")
    assert store.has_observation("identity", stage="post_probe")
    assert not store.has_observation("identity", stage="finalizing")
    with pytest.raises(EvidenceError, match="duplicate_observation_slot"):
        store.observe("identity", {"verification": "overwritten"}, stage="finalizing")
    for stage in ("start", "end"):
        store.observe("environment", {"stage": stage}, stage=stage)
    evidence = TrialEvidence(store.path)
    assert evidence.observation("identity") == {"verification": "fixture"}
    assert evidence.observation("environment", "start") == {"stage": "start"}
    assert evidence.observation("environment", "end") == {"stage": "end"}


def test_prepared_body_is_fsynced_before_start_and_unsent_is_not_execution(make_store, monkeypatch):
    from inferyard.evidence import storage

    make, config, bundle = make_store
    store = make()
    body = request_body(config, bundle["cases"][0]["prompt"])
    seen, original = [], storage.os.fsync

    def fsync(fd):
        seen.append(fd)
        original(fd)

    monkeypatch.setattr(storage.os, "fsync", fsync)
    store.prepare_request("prepared-1", "formal", 1, body)
    assert store._logs["request-bodies.jsonl"].fileno() in seen
    assert read_jsonl(store.path / "events.jsonl") == ([], [])
    seen.clear()
    event = start(store, config, bundle, body)
    assert store._logs["events.jsonl"].fileno() in seen
    store.prepare_request("prepared-unsent", "formal", 2, body)
    evidence = TrialEvidence(store.path)
    assert evidence.request_body(event) == body
    assert len(read_jsonl(store.path / "events.jsonl")[0]) == 1


def test_start_without_successful_prepare_or_matching_message_digest_is_rejected(make_store):
    make, config, bundle = make_store
    store = make()
    body = request_body(config, bundle["cases"][0]["prompt"])
    with pytest.raises(EvidenceError, match="request_snapshot_missing"):
        start(store, config, bundle, body)
    store.prepare_request("prepared-1", "formal", 1, body)
    changed = {**body, "messages": [{"role": "user", "content": "different"}]}
    with pytest.raises(EvidenceError, match="request_messages_hash_mismatch"):
        start(store, config, bundle, changed)
    with pytest.raises(EvidenceError, match="request_body_identity_mismatch"):
        start(store, config, bundle, body, phase="probe")
    assert (store.path / "events.jsonl").read_bytes() == b""


def test_prepared_identity_and_order_cannot_be_reused(make_store):
    make, config, _ = make_store
    store = make()
    body = request_body(config, "fixture")
    store.prepare_request("prepared-1", "probe", 1, body)
    for key, ordinal in (("prepared-1", 2), ("other", 1)):
        with pytest.raises(EvidenceError, match="duplicate_or_misordered_prepared_request"):
            store.prepare_request(key, "probe", ordinal, body)
    assert len(read_jsonl(store.path / "request-bodies.jsonl")[0]) == 1


class FaultyStream:
    def __init__(self, stream, fault):
        self.stream, self.fault = stream, fault

    def write(self, data):
        if self.fault == "append":
            self.stream.write(data[: len(data) // 2])
            self.stream.flush()
            raise OSError("injected append failure")
        return self.stream.write(data)

    def flush(self):
        self.stream.flush()
        if self.fault == "flush":
            raise OSError("injected flush failure")

    def __getattr__(self, name):
        return getattr(self.stream, name)


@pytest.mark.parametrize("method", ["observe", "prepare"])
@pytest.mark.parametrize("fault", ["append", "flush", "fsync"])
def test_failed_append_never_becomes_durable_or_allows_send_or_seal(
    make_store, monkeypatch, method, fault
):
    from inferyard.evidence import storage

    make, config, bundle = make_store
    store = make()
    name = "observations.jsonl" if method == "observe" else "request-bodies.jsonl"
    stream = store._logs[name]
    if fault != "fsync":
        store._logs[name] = FaultyStream(stream, fault)
    else:
        original = storage.os.fsync

        def fsync(fd):
            if fd == stream.fileno():
                raise OSError("injected fsync failure")
            original(fd)

        monkeypatch.setattr(storage.os, "fsync", fsync)
    body = request_body(config, bundle["cases"][0]["prompt"])
    with pytest.raises(EvidenceError):
        if method == "observe":
            store.observe("identity", {"value": "fixture"}, stage="post_probe")
        else:
            store.prepare_request("prepared-1", "formal", 1, body)
    assert not store.has_observation("identity")
    with pytest.raises(EvidenceError):
        start(store, config, bundle, body)
    with pytest.raises(EvidenceError):
        store.seal()
    with pytest.raises(EvidenceError):
        store.seal()
    assert not (store.path / "manifest.json").exists()
    assert (store.path / "events.jsonl").read_bytes() == b""
    rows, tails = read_jsonl(store.path / name)
    assert tails if fault == "append" else len(rows) == 1


def test_manifest_publication_failure_cannot_retry_seal(make_store, monkeypatch):
    from inferyard.evidence import storage

    make, _, _ = make_store
    store = make()
    original = storage.atomic_bytes

    def atomic(path, content, **kwargs):
        if path.name == "manifest.json":
            raise EvidenceError("atomic_write_failed")
        original(path, content, **kwargs)

    monkeypatch.setattr(storage, "atomic_bytes", atomic)
    with pytest.raises(EvidenceError, match="atomic_write_failed"):
        store.seal()
    monkeypatch.setattr(storage, "atomic_bytes", original)
    with pytest.raises(EvidenceError, match="atomic_write_failed"):
        store.seal()
    assert not store.sealed and not (store.path / "manifest.json").exists()
