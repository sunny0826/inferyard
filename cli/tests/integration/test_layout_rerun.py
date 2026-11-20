"""Actual simulated reruns persist embedded source bytes through the production layout2 writer."""

import asyncio
import os
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.application.execution import execute_async
from inferyard.contracts.validation import Document, json_bytes
from inferyard.errors import PreflightError
from inferyard.analysis.ledger import read_trial
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.runtime import runner
from tests.integration.test_runner import scenario as runner_scenario
from tests.unit.layout_surfaces_helpers import tree_bytes

scenario = runner_scenario


@pytest.mark.parametrize("media", ["application/toml", "application/json", None])
def test_container_rerun_writes_actual_source_or_preserves_missing(
    scenario, tmp_path, monkeypatch, media
):
    request, deps, calls, _ = scenario
    if media is None:
        expected = None
        loaded = replace(
            request.config,
            source=None,
            source_data=None,
            source_media_type=None,
            source_missing_reason="not_persisted",
        )
    else:
        if media == "application/toml":
            raw = (
                "# 中文 source spelling\r\n".encode()
                + request.config.source.read_bytes().replace(b"\n", b"\r\n")
            )
            name = "config.input.toml"
        else:
            raw = b"  " + json_bytes(request.config.config.to_dict()) + b"\n"
            name = "config.input.json"
        source = tmp_path / name
        source.write_bytes(raw)
        loaded = replace(
            request.config,
            source=None,
            source_data=source.read_bytes(),
            source_media_type=media,
            source_missing_reason=None,
        )
        expected = raw
    request = replace(request, config=loaded)
    code, original = asyncio.run(execute_async(request, deps))
    assert code == 0, original
    old = Path(original.evidence_dir)
    container = old
    assert TrialEvidence(container).layout_version == 2
    before = {p: tree_bytes(p) for p in (old, container)}
    ticks = request.config.config.to_dict()["endpoint"]["process_start_ticks"]
    monkeypatch.setattr(runner, "process_start_ticks", lambda pid: ticks + 1)
    rerun = CommandRequest(
        "run", from_run=container, server_pid=os.getpid(), endpoint_url="http://127.0.0.1:9090"
    )
    code, result = asyncio.run(execute_async(rerun, deps))
    assert code == 0, result
    out = Path(result.evidence_dir)
    data = read_trial(out)
    assert data["run"]["relation"] == "rerun"
    assert data["run"]["parent_run_id"] == original.run_id
    assert data["summary"]["counts"]["planned"] == len(data["selection"]["case_ids"])
    assert len(calls) == 16
    assert result.run_id != original.run_id
    assert TrialEvidence(out).observation("parent")["path"] == str(container)
    if expected is None:
        assert not (out / "config.input.toml").exists()
        assert not (out / "config.input.json").exists()
        assert TrialEvidence(out).configuration_source() == {
            "media_type": None,
            "data": None,
            "missing_reason": "not_persisted",
        }
    else:
        assert TrialEvidence(out).configuration_source()["data"] == expected
        assert TrialEvidence(out).configuration_source()["media_type"] == media
    assert before == {p: tree_bytes(p) for p in (old, container)}
