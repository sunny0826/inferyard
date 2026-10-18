"""Physical originals and incomplete measurements, using the public command."""

import json
import shutil
from pathlib import Path

import pytest

from tests.integration.context_speed_fixture import scenario, tree_hashes  # noqa: F401
from tests.integration.context_speed_fixture import speed_service as service_fixture
from tests.integration.test_context_speed_cli import invoke, run_speed

speed_service = service_fixture


@pytest.mark.parametrize("mode", ["missing", "counts", "cached", "short"])
def test_measurement_gaps(speed_service, monkeypatch, capsys, mode):
    if mode == "missing":
        speed_service["timing"] = False
    elif mode == "counts":
        speed_service["formal_tokens"] = [100, 101, 102, 103, 104]
    elif mode == "cached":
        speed_service["custom_timing"] = dict(
            cache_n=20, prompt_n=80, prompt_ms=400, predicted_n=11, predicted_ms=500
        )
    else:
        speed_service["formal_output"] = 11
    batch, _ = run_speed(speed_service, monkeypatch, capsys)
    before, calls = tree_hashes(batch), list(speed_service["calls"])
    code, result = invoke(capsys, "speed-results", "--batch", batch)
    assert code == 0, result
    d = result["details"]
    assert d["summary"] is None
    assert d["measurement"]["status"] == "incomplete"
    if mode == "counts":
        assert [s["columns"]["prompt_length"]["value"] for s in d["samples"]] == [
            100,
            101,
            102,
            103,
            104,
        ]
        assert d["measurement"]["valid_count"] == 5
    elif mode == "cached":
        s = d["samples"][0]
        assert [s["columns"][k]["value"] for k in ("time", "prefill", "decode")] == [0.4, 200, 20]
        assert "cold_cache_unproven" in s["measurement"]["reasons"]
    else:
        assert d["measurement"]["valid_count"] == 0
    assert before == tree_hashes(batch) and calls == speed_service["calls"]


@pytest.mark.parametrize(
    "mode", ["hash", "seal", "other_request", "event_seq", "other_run", "member"]
)
def test_damaged_copy_returns_only_error(speed_service, monkeypatch, capsys, mode):
    batch, _ = run_speed(speed_service, monkeypatch, capsys)
    original = tree_hashes(batch)
    copy = batch.parent / "damaged"
    shutil.copytree(batch, copy)
    runs = sorted((copy / "runs").iterdir())
    run = runs[0]
    if mode == "hash":
        with (run / "events.jsonl").open("ab") as out:
            out.write(b"\n")
    elif mode == "seal":
        (run / "manifest.json").write_text("{}")
    elif mode == "other_run":
        shutil.copyfile(runs[1] / "inputs.json", run / "inputs.json")
    else:
        path = run / ("request-bodies.jsonl" if mode == "member" else "events.jsonl")
        rows = [json.loads(line) for line in path.read_text().splitlines()]
        if mode == "member":
            rows[-1]["request_id"] = rows[0]["request_id"]
        else:
            event = next(
                r for r in rows if r["event_type"] == "engine_timings" and r["phase"] == "formal"
            )
            if mode == "event_seq":
                event["seq"] = 1
            else:
                event["request_id"] = rows[0]["request_id"]
        path.write_text("".join(json.dumps(r) + "\n" for r in rows))
    before, calls = tree_hashes(copy), list(speed_service["calls"])
    code, result = invoke(capsys, "speed-results", "--batch", copy)
    assert code == 4, result
    assert set(result["details"]) == {"schema_version", "kind", "read_state", "error"}
    assert before == tree_hashes(copy) and calls == speed_service["calls"]
    assert original == tree_hashes(batch)


def test_partial_and_zero_copy(speed_service, monkeypatch, capsys):
    batch, execution = run_speed(speed_service, monkeypatch, capsys)
    original = tree_hashes(batch)
    for keep in (0, 2):
        copy = batch.parent / f"partial-{keep}"
        shutil.copytree(batch, copy)
        for source in execution["details"]["runs"][keep:]:
            shutil.rmtree(copy / "runs" / Path(source).name)
        before = tree_hashes(copy)
        code, result = invoke(capsys, "speed-results", "--batch", copy)
        assert code == 3, result
        d = result["details"]
        assert d["execution"]["counts"]["completed"] == keep
        assert d["execution"]["counts"]["not_executed"] == 5 - keep
        assert d["summary"] is None
        if not keep:
            assert d["execution"]["stop_reason"]["missing_reason"] == "unknown"
        assert before == tree_hashes(copy)
        calls = list(speed_service["calls"])
        code, _ = invoke(
            capsys, "run", "--plan", copy / "frozen-plan/plan.json", "--output-root", copy
        )
        assert code == 2
        assert before == tree_hashes(copy) and calls == speed_service["calls"]
    assert original == tree_hashes(batch)


def test_forbidden_reexecution(speed_service, monkeypatch, capsys):
    batch, execution = run_speed(speed_service, monkeypatch, capsys)
    before, calls = tree_hashes(batch), list(speed_service["calls"])
    run = execution["details"]["runs"][0]
    for args in [
        ("resume", "--from-run", run),
        (
            "run",
            "--rerun-from",
            run,
            "--endpoint-url",
            speed_service["config"]["endpoint"]["url"],
            "--server-pid",
            "123",
            "--output-root",
            speed_service["root"] / "must-not-exist",
        ),
        ("run", "--config", speed_service["write"]()),
    ]:
        code, _ = invoke(capsys, *args)
        assert code == 2
        assert before == tree_hashes(batch) and calls == speed_service["calls"]
    assert not (speed_service["root"] / "must-not-exist").exists()
    from inferyard.application.types import CommandRequest
    from inferyard.platforms.identity import PreflightError
    from inferyard.runtime.runner import load_rerun

    with pytest.raises(PreflightError, match="context_speed_requires_new_plan_and_batch"):
        load_rerun(CommandRequest("run", from_run=Path(run)))


def test_timing_cannot_override_actual_output_count(speed_service, monkeypatch, capsys):
    speed_service["formal_output"] = 11
    speed_service["custom_timing"] = dict(
        cache_n=0, prompt_n=100, prompt_ms=400, predicted_n=256, predicted_ms=500
    )
    batch, _ = run_speed(speed_service, monkeypatch, capsys)
    code, result = invoke(capsys, "speed-results", "--batch", batch)
    assert code == 0
    assert result["details"]["summary"] is None
    assert result["details"]["measurement"]["valid_count"] == 0
    assert "fixed_output_unverified" in result["details"]["samples"][0]["measurement"]["reasons"]
