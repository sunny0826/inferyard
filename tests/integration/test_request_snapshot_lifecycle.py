"""Both live writers seal portable filenames that offline replay and reports can read."""

import asyncio
from pathlib import Path

import pytest

from inferyard.evidence.ledger import read_trial
from inferyard.evidence.request_snapshots import messages_sha256, snapshot_filename
from inferyard.evidence.storage import (
    EvidenceError,
    json_bytes,
    read_json,
    read_jsonl,
    verify_manifest,
)
from inferyard.runtime.runner import execute_async
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_runner import scenario
from tests.integration.test_trial_runner import inputs

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
    starts = [
        event
        for event in read_jsonl(root / "events.jsonl")[0]
        if event["event_type"] == "request_started"
    ]
    expected = [
        snapshot_filename(event["run_id"], event["phase"], ordinal)
        for ordinal, event in enumerate(starts, 1)
    ]
    actual = sorted(path.name for path in root.glob("*.request.json"))
    assert actual == expected and len(actual) == len(calls)
    manifest = read_json(root / "manifest.json")["files"]
    for name, body in zip(actual, calls, strict=True):
        assert ":" not in name and name in manifest
        assert read_json(root / name) == body
    for event, body in zip(starts, calls, strict=True):
        assert "messages" not in event["data"]["body"]
        assert event["data"]["body"]["messages_sha256"] == messages_sha256(body["messages"])


@pytest.mark.parametrize("change", ["missing", "messages", "hash"])
def test_new_event_requires_matching_original_snapshot(scenario, change):
    request, dependencies, _, _ = scenario
    code, result = asyncio.run(execute_async(request, dependencies))
    assert code == 0
    root = Path(result.evidence_dir)
    (root / "manifest.json").unlink()  # Exercise the binding, independently of seal rejection.
    events = read_jsonl(root / "events.jsonl")[0]
    start = next(e for e in events if e["event_type"] == "request_started")
    snapshot = root / snapshot_filename(start["run_id"], start["phase"], 1)
    if change == "missing":
        snapshot.unlink()
    elif change == "messages":
        body = read_json(snapshot)
        body["messages"][0]["content"] = "different original body"
        snapshot.write_bytes(json_bytes(body))
    else:
        start["data"]["body"]["messages_sha256"] = "0" * 64
        (root / "events.jsonl").write_bytes(b"".join(json_bytes(e) for e in events))
    with pytest.raises(
        EvidenceError, match="request_snapshot_missing|request_messages_hash_mismatch"
    ):
        read_trial(root)
