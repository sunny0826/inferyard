"""Both live writers seal portable filenames that offline replay and reports can read."""

import asyncio
from pathlib import Path

import pytest

from inferyard.evidence.ledger import read_trial
from inferyard.evidence.request_snapshots import snapshot_filename
from inferyard.evidence.storage import read_json, read_jsonl, verify_manifest
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
