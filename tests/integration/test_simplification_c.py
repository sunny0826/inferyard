"""C scoped identities and raw durability using the existing simulated service."""

import asyncio
from collections import Counter
from copy import deepcopy
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.contracts.validation import Document
from inferyard.evidence import storage
from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import json_bytes, read_json, read_jsonl
from inferyard.platforms.identity import PreflightError
from inferyard.runtime import runner
from tests.integration.test_runner import scenario as runner_scenario

scenario = runner_scenario


def test_raw_terminal_and_scores_sync_without_derived_request_copy(scenario, monkeypatch):
    request, deps, _, _ = scenario
    writes, syncs = Counter(), []
    original_event, original_sync = TrialJournal.event, storage.os.fsync

    def fsync(fd):
        syncs.append(fd)
        return original_sync(fd)

    def event(self, kind, *args, **kwargs):
        before = len(syncs)
        result = original_event(self, kind, *args, **kwargs)
        if kind in ("request_finished", "score"):
            assert self._logs["events.jsonl"].fileno() in syncs[before:]
            writes[kind] += 1
        return result

    monkeypatch.setattr(storage.os, "fsync", fsync)
    monkeypatch.setattr(TrialJournal, "event", event)
    code, result = asyncio.run(runner.execute_async(request, deps))
    assert code == 0
    root = Path(result.evidence_dir)
    assert writes == {"request_finished": 8, "score": 3}
    assert not (root / "requests.jsonl").exists()
    assert "requests.jsonl" not in read_json(root / "manifest.json")["files"]
    assert len(read_trial(root)["requests"]) == 3


def test_score_append_failure_preserves_durable_completed_terminal(scenario, monkeypatch):
    request, deps, _, _ = scenario
    original = TrialJournal.event
    seen = []

    def event(self, kind, *args, **kwargs):
        if kind == "score":
            events, issues = read_jsonl(self.path / "events.jsonl")
            assert not issues
            terminals = [
                e
                for e in events
                if e["event_type"] == "request_finished" and e["phase"] == "formal"
            ]
            assert len(terminals) == 1
            seen.append(terminals[0])
            raise storage.EvidenceError("append_failed")
        return original(self, kind, *args, **kwargs)

    monkeypatch.setattr(TrialJournal, "event", event)
    code, result = asyncio.run(runner.execute_async(request, deps))
    assert code == 4 and len(seen) == 1
    data = read_trial(Path(result.evidence_dir))
    assert data["summary"]["counts"]["completed"] == 1
    assert data["summary"]["counts"]["not_executed"] == 2
    assert data["requests"][0]["execution_state"] == "completed"
    assert not (Path(result.evidence_dir) / "requests.jsonl").exists()


def test_new_and_legacy_rerun_accept_identity_changes_with_new_service(scenario, monkeypatch):
    request, deps, _, _ = scenario
    config = request.config.config.to_dict()
    config["execution"]["require_fresh_process"] = False
    request = replace(
        request, config=replace(request.config, config=Document.parse("config", config))
    )
    code, result = asyncio.run(runner.execute_async(request, deps))
    assert code == 0
    root = Path(result.evidence_dir)
    endpoint = config["endpoint"]
    rerun = CommandRequest(
        "run", from_run=root, server_pid=endpoint["server_pid"] + 1, endpoint_url=endpoint["url"]
    )

    def ticks(pid):
        if pid == endpoint["server_pid"]:
            raise PreflightError("service_process_unavailable")
        return 98765

    monkeypatch.setattr(runner, "process_start_ticks", ticks)
    from inferyard.implementation_identity import IdentityContext, digest

    context = IdentityContext()
    context.source = "b" * 64
    value = deepcopy(context.value["presentation"])
    value["files"][0]["sha256"] = "c" * 64
    value["sha256"] = digest({k: v for k, v in value.items() if k != "sha256"})
    context.value["presentation"] = value
    runner.load_rerun(rerun, identity_context=context)
    # A new directory containing a valid legacy envelope exercises legacy dispatch.
    meta = read_json(root / "run.json")
    meta.pop("implementation_identity")
    (root / "run.json").write_bytes(json_bytes(meta))
    manifest = read_json(root / "manifest.json")
    manifest["files"]["run.json"].update(
        sha256=storage.sha256_file(root / "run.json"), bytes=(root / "run.json").stat().st_size
    )
    (root / "manifest.json").write_bytes(json_bytes(manifest))
    runner.load_rerun(rerun, identity_context=context)


def test_batch_continuation_dispatches_scoped_and_legacy_identity(scenario, tmp_path, monkeypatch):
    from inferyard.implementation_identity import IdentityContext
    from inferyard.runtime import batch_runner
    from tests.integration.test_batch_runner import batch

    request, deps = batch(tmp_path, scenario)
    code, result = asyncio.run(batch_runner.execute_async(request, deps))
    assert code == 0 and len(result.details["runs"]) == 3
    context = IdentityContext()
    context.source = "b" * 64  # e.g. help/HTML changed; execution roles stay the same.
    monkeypatch.setattr(batch_runner, "IdentityContext", lambda **kw: context)
    before = len(scenario[2])
    code, repeated = asyncio.run(batch_runner.execute_async(request, deps))
    assert code == 0 and len(scenario[2]) == before
    assert len(repeated.details["runs"]) == 3
    meta_path = request.output_root / "batch.json"
    metadata = read_json(meta_path)
    metadata.pop("implementation_identity")
    meta_path.write_bytes(json_bytes(metadata))
    with pytest.raises(storage.EvidenceError, match="batch_identity_or_tool_changed"):
        asyncio.run(batch_runner.execute_async(request, deps))
    assert len(scenario[2]) == before
