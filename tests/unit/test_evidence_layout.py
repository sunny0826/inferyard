"""Public domain reads and strict physical/member bindings on synthetic originals."""

import copy

import pytest

from inferyard.adapters.requests import request_body
from inferyard.config.loader import load_config
from inferyard.config.single_plan import compile_single_plan
from inferyard.evidence.formats import UnsupportedFormat
from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.request_snapshots import (
    messages_sha256,
    snapshot_filename,
    verify_message_snapshots,
)
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json, read_jsonl, sha256_file
from inferyard.evidence.trial_evidence import TrialEvidence, verify_reference
from tests.evidence_layout_helpers import digest, pack_layout2, seal_layout


@pytest.fixture
def originals(tmp_path, config_path):
    loaded = load_config(config_path)
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    plan = compile_single_plan(config, bundle)
    store = TrialJournal(
        tmp_path / "old",
        plan,
        plan["trials"][0]["trial_id"],
        config,
        bundle,
        run_id="synthetic-layout",
        diagnostic=True,
    )
    store.text_snapshot("config.input.toml", "# 原字节\nschema_version = 3\n")
    for phase, ordinal, stream in (("probe", 1, False), ("probe", 2, True), ("formal", 3, True)):
        body = request_body(config, "synthetic original", stream=stream)
        key = f"{store.run_id}-{phase}-{ordinal}"
        store.snapshot(snapshot_filename(store.run_id, phase, ordinal), body)
        store.event(
            "request_started",
            phase,
            key,
            {
                "case_id": bundle["cases"][0]["case_id"] if phase == "formal" else None,
                "plan_index": 0 if phase == "formal" else None,
                "attempt": 1,
                "body": {
                    "model": body["model"],
                    "messages_sha256": messages_sha256(body["messages"]),
                    "stream": stream,
                    "generation": config["generation"],
                },
            },
        )
    store.snapshot("environment.start.json", {"value": "original-start"})
    store.snapshot("environment.end.json", {"value": "original-end"})
    store.snapshot("effective-ordinary.json", {"value": "ordinary"})
    store.snapshot("effective-stream.json", {"value": "stream"})
    store.snapshot("identity.json", {"value": "authoritative"})
    store.snapshot(snapshot_filename(store.run_id, "formal", 4), body)  # Prepared, never sent.
    store.seal()
    store.close()
    new = pack_layout2(store.path, tmp_path / "new")
    return store.path, new


def write_rows(root, name, rows):
    (root / name).write_bytes(b"".join(map(json_bytes, rows)))
    seal_layout(root, read_json(root / "inputs.json")["documents"]["run"]["payload"])


def test_original_members_requests_and_references_round_trip(originals):
    old, new = originals
    before = {path.name: path.read_bytes() for path in old.iterdir()}
    legacy, packed = TrialEvidence(old), TrialEvidence(new)
    assert legacy.layout_version == 1 and packed.layout_version == 2
    assert len(list(new.iterdir())) == 8
    for kind in ("run", "plan", "selection", "config", "bundle"):
        assert legacy.input(kind) == packed.input(kind)
        assert legacy.input_digest(kind) == packed.input_digest(kind)
        verify_reference(new, packed.input_reference(kind))
        verify_reference(old, legacy.input_reference(kind))
    assert legacy.configuration_source() == packed.configuration_source()
    for kind, stage in (
        ("environment", "start"),
        ("environment", "end"),
        ("effective_parameters", "ordinary"),
        ("effective_parameters", "stream"),
    ):
        assert legacy.observation(kind, stage) == packed.observation(kind, stage)
        verify_reference(new, packed.observation_reference(kind, stage))
    events, _ = read_jsonl(old / "events.jsonl")
    for event in events:
        assert legacy.request_body(event) == packed.request_body(event)
        verify_reference(new, packed.request_reference(event))
        verify_reference(old, legacy.request_reference(event))
    verify_message_snapshots(new, events, packed)
    packed.preload(packed.manifest["files"])
    assert set(packed.hashes) <= {path.name for path in new.iterdir()}
    assert packed.hashes["inputs.json"] == sha256_file(new / "inputs.json")
    assert packed.input_digest("config") != packed.hashes["inputs.json"]
    assert before == {path.name: path.read_bytes() for path in old.iterdir()}


@pytest.mark.parametrize(
    "damage", ["unknown", "conflict", "mixed", "duplicate_json", "hash", "bool"]
)
def test_layout_and_input_corruption_is_not_a_legacy_fallback(originals, damage):
    _, root = originals
    inputs = read_json(root / "inputs.json")
    if damage == "unknown":
        inputs["evidence_layout_version"] = 99
        manifest = read_json(root / "manifest.json")
        manifest["evidence_layout_version"] = 99
        (root / "manifest.json").write_bytes(json_bytes(manifest))
    elif damage == "conflict":
        inputs["evidence_layout_version"] = 99
    elif damage == "mixed":
        (root / "run.json").write_bytes(json_bytes(inputs["documents"]["run"]["payload"]))
    elif damage == "hash":
        inputs["documents"]["config"]["sha256"] = "0" * 64
    elif damage == "bool":
        inputs["evidence_layout_version"] = True
    (root / "inputs.json").write_bytes(json_bytes(inputs))
    if damage == "duplicate_json":
        raw = (root / "inputs.json").read_bytes()
        (root / "inputs.json").write_bytes(b'{"schema_version":3,' + raw[1:])
    with pytest.raises(UnsupportedFormat if damage == "unknown" else EvidenceError):
        TrialEvidence(root)


@pytest.mark.parametrize("damage", ["duplicate", "kind", "stage", "key", "seq", "identity", "hash"])
def test_observation_slots_and_records_are_closed(originals, damage):
    _, root = originals
    rows, _ = read_jsonl(root / "observations.jsonl")
    if damage == "duplicate":
        rows.append({**rows[0], "seq": len(rows) + 1})
    elif damage == "identity":
        rows.append({**rows[-1], "seq": len(rows) + 1, "stage": "finalizing"})
    else:
        field = {
            "kind": "kind",
            "stage": "stage",
            "key": "key",
            "seq": "seq",
            "hash": "payload_sha256",
        }[damage]
        rows[0][field] = {
            "kind": "unknown",
            "stage": "finalizing",
            "key": "bad-key",
            "seq": True,
            "hash": "0" * 64,
        }[damage]
    write_rows(root, "observations.jsonl", rows)
    with pytest.raises(EvidenceError):
        TrialEvidence(root).observation("environment", "start")


@pytest.mark.parametrize(
    "damage", ["duplicate", "ordinal", "phase", "identity", "bodyhash", "messages"]
)
def test_prepared_records_are_indexed_and_validated_before_any_body_read(originals, damage):
    _, root = originals
    rows, _ = read_jsonl(root / "request-bodies.jsonl")
    if damage == "duplicate":
        rows.append(rows[0])
    elif damage == "ordinal":
        rows[1]["ordinal"] = rows[0]["ordinal"]
    elif damage == "phase":
        rows[-1]["phase"] = "baseline"
    elif damage == "identity":
        rows[-1]["trial_id"] = "other"
    elif damage == "bodyhash":
        rows[-1]["body_sha256"] = "0" * 64
    else:
        del rows[-1]["body"]["messages"]
        rows[-1]["body_sha256"] = digest(rows[-1]["body"])
    write_rows(root, "request-bodies.jsonl", rows)
    events, _ = read_jsonl(root / "events.jsonl")
    with pytest.raises(EvidenceError):
        TrialEvidence(root).request_body(events[0])


def test_request_messages_digest_is_bound_to_original_body(originals):
    _, root = originals
    events, _ = read_jsonl(root / "events.jsonl")
    events[0]["data"]["body"]["messages_sha256"] = "0" * 64
    with pytest.raises(EvidenceError, match="request_messages_hash_mismatch"):
        verify_message_snapshots(root, events, TrialEvidence(root))


def test_missing_started_original_and_prepared_unsent_are_distinct(originals):
    _, root = originals
    rows, _ = read_jsonl(root / "request-bodies.jsonl")
    assert len(rows) == 4
    write_rows(root, "request-bodies.jsonl", rows[1:])
    events, _ = read_jsonl(root / "events.jsonl")
    with pytest.raises(EvidenceError, match="request_snapshot_missing"):
        TrialEvidence(root).request_body(events[0])


def test_byte_range_reread_rejects_changed_original(originals):
    _, root = originals
    evidence = TrialEvidence(root)
    events, _ = read_jsonl(root / "events.jsonl")
    assert evidence.request_body(events[0])
    raw = (root / "request-bodies.jsonl").read_bytes()
    (root / "request-bodies.jsonl").write_bytes(
        raw.replace(b"synthetic original", b"synthetic tampered")
    )
    with pytest.raises(EvidenceError, match="request_body_changed"):
        evidence.request_body(events[0])


@pytest.mark.parametrize("sealed", [False, True])
def test_only_unsealed_tail_retains_partial_state(originals, sealed):
    _, root = originals
    if not sealed:
        (root / "manifest.json").unlink()
    with (root / "request-bodies.jsonl").open("ab") as stream:
        stream.write(b'{"schema_version":')
    evidence = TrialEvidence(root)
    events, _ = read_jsonl(root / "events.jsonl")
    if sealed:
        with pytest.raises(EvidenceError):
            evidence.request_body(events[0])
    else:
        assert evidence.request_body(events[0])
        assert evidence._prepared.tails


def test_member_reference_never_substitutes_member_hash_for_file_hash(originals):
    _, root = originals
    evidence = TrialEvidence(root)
    ref = evidence.input_reference("config")
    damaged = copy.deepcopy(ref)
    damaged["sha256"] = ref["member"]["sha256"]
    with pytest.raises(EvidenceError):
        verify_reference(root, damaged)
    damaged = copy.deepcopy(ref)
    damaged["member"]["sha256"] = "0" * 64
    with pytest.raises(EvidenceError):
        verify_reference(root, damaged)


def test_projection_does_not_scan_unused_prepared_or_observation_payloads(originals):
    _, root = originals
    (root / "request-bodies.jsonl").write_bytes(b"broken\n")
    (root / "observations.jsonl").write_bytes(b"broken\n")
    evidence = TrialEvidence(root, projection=True)
    evidence.preload(evidence.manifest["files"])
    assert evidence.input("run")["run_id"] == "synthetic-layout"
    assert "request-bodies.jsonl" not in evidence.hashes
    assert "observations.jsonl" not in evidence.hashes
