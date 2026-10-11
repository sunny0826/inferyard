"""Real temporary-file checks for the production layout 2 durability boundary."""

import hashlib
import json

import pytest

from inferyard.adapters.requests import request_body
from inferyard.config.loader import load_config
from inferyard.config.single_plan import compile_single_plan
from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.request_snapshots import messages_sha256
from inferyard.evidence.storage import EvidenceError, EvidenceStore, Redactor, read_json, read_jsonl
from inferyard.evidence.trial_evidence import TrialEvidence, verify_reference


@pytest.fixture
def make_store(tmp_path, config_path):
    loaded = load_config(config_path)
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    plan = compile_single_plan(config, bundle)
    stores = []

    def make(**kwargs):
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


def test_inputs_publish_before_all_six_logs_and_seal_eight_files(make_store, monkeypatch):
    make, _, _ = make_store
    original = EvidenceStore._open_logs
    seen = []

    def open_logs(store, names):
        seen.append({path.name for path in store.path.iterdir()})
        assert seen[-1] == {"inputs.json"}
        original(store, names)

    monkeypatch.setattr(EvidenceStore, "_open_logs", open_logs)
    store = make()
    assert seen == [{"inputs.json"}]
    assert len(list(store.path.iterdir())) == 7
    store.seal()
    assert store.sealed and len(list(store.path.iterdir())) == 8
    evidence = TrialEvidence(store.path)
    assert evidence.layout_version == evidence.manifest["evidence_layout_version"] == 2
    for kind in ("run", "plan", "selection", "config", "bundle"):
        verify_reference(store.path, evidence.input_reference(kind))
    evidence.preload(evidence.manifest["files"])
    with pytest.raises(EvidenceError, match="sealed_run"):
        store.seal()


@pytest.mark.parametrize("opened", [0, 3, 5])
def test_interrupted_log_creation_reads_only_real_partial_evidence(
    make_store, tmp_path, monkeypatch, capsys, opened
):
    from inferyard.cli import main
    from inferyard.evidence.ledger import read_trial

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
    from inferyard.evidence.ledger import read_trial

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


@pytest.mark.parametrize(
    "source",
    [
        {},
        {"media_type": None, "data": None, "missing_reason": None},
        {"media_type": "application/toml", "data": "parsed text", "missing_reason": None},
        {"media_type": "application/toml", "data": b"raw", "missing_reason": "invented"},
    ],
)
def test_invalid_source_cannot_produce_a_live_writer(make_store, source):
    make, _, _ = make_store
    with pytest.raises(EvidenceError, match="invalid_inputs_container"):
        make(configuration_source=source)


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


@pytest.mark.parametrize(
    "kind,stage,key",
    [
        ("unknown", "prepare", None),
        ("collector", "finalizing", None),
        ("collector", "prepare", "bad-key"),
        ("service_properties", "pre_idle", None),
    ],
)
def test_invalid_observation_never_appends_or_occupies_a_slot(make_store, kind, stage, key):
    make, _, _ = make_store
    store = make()
    with pytest.raises(EvidenceError):
        store.observe(kind, {"value": "invalid"}, stage=stage, key=key)
    assert (store.path / "observations.jsonl").read_bytes() == b""
    assert store._observation_slots == {}


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
    assert all(
        len(metadata) == 3 and isinstance(metadata[2], str)
        for metadata in store._prepared_requests.values()
    )


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


@pytest.mark.parametrize("phase,ordinal", [("baseline", 1), ("formal", True), ("formal", 0)])
def test_prepared_phase_and_ordinal_are_strict(make_store, phase, ordinal):
    make, config, _ = make_store
    store = make()
    with pytest.raises(EvidenceError, match="invalid_prepared_request"):
        store.prepare_request("prepared-1", phase, ordinal, request_body(config, "fixture"))
    assert (store.path / "request-bodies.jsonl").read_bytes() == b""


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
    assert "prepared-1" not in store._prepared_requests
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


def test_run_snapshot_guard_and_high_frequency_logs_remain_distinct(make_store):
    make, _, _ = make_store
    store = make()
    for name in ("run.json", "identity.json", "collector.json", "config.input.toml"):
        with pytest.raises(EvidenceError, match="invalid_observation_record"):
            store.snapshot(name, {"old": "snapshot"})
    with pytest.raises(EvidenceError):
        store.text_snapshot("config.input.toml", "raw")
    for name in ("external-cpu.jsonl", "request-environment.jsonl", "safety-checks.jsonl"):
        store.observation(name, {"value": "actual-high-frequency"})
    store.seal()
    assert all(
        name in read_json(store.path / "manifest.json")["files"]
        for name in ("external-cpu.jsonl", "request-environment.jsonl", "safety-checks.jsonl")
    )


def test_generic_and_extension_stores_keep_independent_protocol(tmp_path):
    from inferyard.extensions.extension_evidence import ExtensionJournal

    for constructor in (EvidenceStore, ExtensionJournal):
        store = constructor(tmp_path)
        store.snapshot("run.json", {"kind": "independent"})
        store.observe("collector", {"collector": "actual"}, stage="prepare")
        with pytest.raises(EvidenceError, match="invalid_observation_record"):
            store.observe("identity", {}, stage="finalizing")
        store.seal()
        store.close()
        assert read_json(store.path / "collector.json") == {"collector": "actual"}
        assert not (store.path / "inputs.json").exists()
        assert not (store.path / "observations.jsonl").exists()
        assert not (store.path / "request-bodies.jsonl").exists()


def test_baseline_domain_methods_preserve_original_buffer_and_snapshot_keys(make_store, tmp_path):
    from inferyard.evidence.request_snapshots import snapshot_filename
    from inferyard.extensions.trial_control_baseline import BaselineJournal

    make, config, bundle = make_store
    store = make()
    plan = TrialEvidence(store.path).input("plan")
    baseline = BaselineJournal(tmp_path, plan, store.trial_id, config, bundle, redactor=Redactor())
    baseline.observe("environment", {"value": "start"}, stage="start")
    baseline.observe("identity", {"value": "identity"}, stage="finalizing")
    body = request_body(config, "fixture")
    baseline.prepare_request("baseline-body", "probe", 1, body)
    assert baseline.has_observation("identity", stage="finalizing")
    buffer = baseline.buffer()
    assert buffer["definition"] == "trial_control_baseline.v1"
    assert buffer["snapshots"]["environment.start.json"] == {"value": "start"}
    assert buffer["snapshots"]["identity.json"] == {"value": "identity"}
    assert buffer["snapshots"][snapshot_filename(baseline.run_id, "probe", 1)] == body
    assert "inputs.json" not in buffer["snapshots"]
    baseline.seal()
    with pytest.raises(EvidenceError, match="sealed_run"):
        baseline.observe("environment", {"value": "end"}, stage="end")
