"""Actual simulated reruns persist embedded source bytes through the legacy writer."""

import asyncio
import os
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.contracts.validation import Document
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import json_bytes, read_json
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.platforms.identity import PreflightError
from inferyard.runtime import runner
from tests.evidence_layout_helpers import pack_layout2
from tests.integration.test_runner import scenario as runner_scenario
from tests.unit.test_layout_surfaces import tree_bytes

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
    code, original = asyncio.run(runner.execute_async(request, deps))
    assert code == 0, original
    old = Path(original.evidence_dir)
    container = pack_layout2(old, tmp_path / "container")
    before = {p: tree_bytes(p) for p in (old, container)}
    ticks = request.config.config.to_dict()["endpoint"]["process_start_ticks"]
    # Synthetic process replacement only; real process qualification is separate.
    monkeypatch.setattr(runner, "process_start_ticks", lambda pid: ticks + 1)
    rerun = CommandRequest(
        "run", from_run=container, server_pid=os.getpid(), endpoint_url="http://127.0.0.1:9090"
    )
    reloaded, _ = runner.load_rerun(rerun)
    assert reloaded.source is None
    assert reloaded.configuration_source()["data"] == expected
    code, result = asyncio.run(runner.execute_async(rerun, deps))
    assert code == 0, result
    out = Path(result.evidence_dir)
    data = read_trial(out)
    assert data["run"]["relation"] == "rerun"
    assert data["run"]["parent_run_id"] == original.run_id
    assert data["summary"]["counts"]["planned"] == len(data["selection"]["case_ids"])
    assert len(calls) == 16
    assert result.run_id != original.run_id
    assert read_json(out / "parent.json")["path"] == str(container)
    if expected is None:
        assert not (out / "config.input.toml").exists()
        assert not (out / "config.input.json").exists()
        assert TrialEvidence(out).configuration_source() == {
            "media_type": None,
            "data": None,
            "missing_reason": "not_persisted",
        }
    else:
        assert (out / name).read_bytes() == expected
        assert TrialEvidence(out).configuration_source()["media_type"] == media
    assert before == {p: tree_bytes(p) for p in (old, container)}


@pytest.mark.parametrize(
    "data,media,reason",
    [
        (b"\xff", "application/toml", "configuration_source_utf8_invalid"),
        (b"{}", "text/plain", "configuration_source_media_type_invalid"),
    ],
)
def test_invalid_memory_source_is_rejected_before_creating_run(scenario, data, media, reason):
    request, deps, calls, _ = scenario
    loaded = replace(
        request.config,
        source=None,
        source_data=data,
        source_media_type=media,
        source_missing_reason=None,
    )
    request = replace(request, config=loaded)
    with pytest.raises(PreflightError, match=reason):
        asyncio.run(runner.execute_async(request, deps))
    assert not Path(loaded.config.to_dict()["output"]["root"]).exists()
    assert not calls


def test_memory_source_uses_existing_text_snapshot_redaction(scenario, monkeypatch):
    request, deps, calls, _ = scenario
    secret = "fixture-source-private-value"
    monkeypatch.setenv("INFERYARD_FIXTURE_SOURCE_KEY", secret)
    config = request.config.config.to_dict()
    config["endpoint"]["api_key_env"] = "INFERYARD_FIXTURE_SOURCE_KEY"
    raw = ("# " + secret + "\r\n").encode() + request.config.source.read_bytes()
    loaded = replace(
        request.config,
        source=None,
        source_data=raw,
        source_media_type="application/toml",
        source_missing_reason=None,
        config=Document.parse("config", config),
    )
    code, result = asyncio.run(runner.execute_async(replace(request, config=loaded), deps))
    assert code == 0, result
    out = Path(result.evidence_dir)
    assert (out / "config.input.toml").read_bytes() == raw.replace(secret.encode(), b"[REDACTED]")
    assert secret.encode() not in (out / "config.input.toml").read_bytes()
    assert len(calls) == 8
    assert read_trial(out)["summary"]["counts"]["planned"] == 3
