"""Live writers persist indexed bodies before sending; replay checks their original bytes."""

import asyncio
from pathlib import Path

import pytest

from inferyard.evidence.ledger import read_trial
from inferyard.evidence.request_snapshots import messages_sha256
from inferyard.evidence.storage import EvidenceError, json_bytes, read_jsonl, verify_manifest
from inferyard.evidence.trial_evidence import TrialEvidence, verify_reference
from inferyard.runtime.runner import execute_async
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_runner import scenario
from tests.integration.test_trial_runner import inputs
from tests.layout_mutations import change_request

__all__ = ["scenario"]


@pytest.mark.parametrize("mode", ["single", "experiment"])
def test_writers_seal_body_snapshots_in_send_order(scenario, mode):
    request, dependencies, calls, _ = scenario
    if mode == "single":
        code, result = asyncio.run(execute_async(request, dependencies))
        root = Path(result.evidence_dir)
    else:
        plan, loaded, dependencies, out = inputs(scenario)
        code, _, root = asyncio.run(
            run_trial(
                plan,
                plan["trials"][0]["trial_id"],
                loaded,
                out,
                diagnostic=True,
                dependencies=dependencies,
            )
        )
        assert read_trial(root)["summary"]["counts"]["completed"] == 3
    assert code == 0 and verify_manifest(root) == []
    evidence = TrialEvidence(root)
    assert evidence.layout_version == 2
    starts = [
        event
        for event in read_jsonl(root / "events.jsonl")[0]
        if event["event_type"] == "request_started"
    ]
    prepared, tails = read_jsonl(root / "request-bodies.jsonl")
    assert not tails and len(prepared) == len(calls) == len(starts)
    assert [row["ordinal"] for row in prepared] == list(range(1, len(calls) + 1))
    assert [row["request_id"] for row in prepared] == [event["request_id"] for event in starts]
    assert not list(root.glob("*.request.json"))
    for event, row, body in zip(starts, prepared, calls, strict=True):
        assert evidence.request_body(event) == row["body"] == body
        assert row["phase"] == event["phase"]
        verify_reference(root, evidence.request_reference(event), reads=evidence)
        assert "messages" not in event["data"]["body"]
        assert event["data"]["body"]["messages_sha256"] == messages_sha256(body["messages"])


@pytest.mark.parametrize("change", ["missing", "messages", "hash"])
def test_new_event_requires_matching_original_snapshot(scenario, change):
    request, dependencies, _, _ = scenario
    code, result = asyncio.run(execute_async(request, dependencies))
    assert code == 0
    root = Path(result.evidence_dir)
    (root / "manifest.json").unlink()
    events = read_jsonl(root / "events.jsonl")[0]
    start = next(event for event in events if event["event_type"] == "request_started")
    if change == "missing":
        change_request(root, request_id=start["request_id"], remove=True)
    elif change == "messages":
        change_request(
            root,
            lambda body: body["messages"][0].update(content="different original"),
            request_id=start["request_id"],
        )
    else:
        start["data"]["body"]["messages_sha256"] = "0" * 64
        (root / "events.jsonl").write_bytes(b"".join(json_bytes(event) for event in events))
    with pytest.raises(
        EvidenceError, match="request_snapshot_missing|request_messages_hash_mismatch"
    ):
        read_trial(root)
