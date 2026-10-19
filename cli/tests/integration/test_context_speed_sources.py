"""Semantic forgeries in isolated copies, with their file hashes recomputed.

These intentionally malicious copies are not measurement evidence. Originals are kept intact.
"""

import hashlib
import json
import shutil
from pathlib import Path

import pytest

from inferyard.evidence.storage import json_bytes
from tests.integration.context_speed_fixture import scenario, tree_hashes  # noqa: F401
from tests.integration.context_speed_fixture import speed_service as service_fixture
from tests.integration.test_context_speed_cli import invoke, run_speed

speed_service = service_fixture


def rewrite(run, name, rows):
    raw = b"".join(
        json.dumps(row, ensure_ascii=False, allow_nan=False).encode() + b"\n" for row in rows
    )
    (run / name).write_bytes(raw)
    manifest = json.loads((run / "manifest.json").read_text())
    manifest["files"][name].update(bytes=len(raw), sha256=hashlib.sha256(raw).hexdigest())
    (run / "manifest.json").write_bytes(json_bytes(manifest))


@pytest.mark.parametrize(
    "change", ["sequence", "request", "member", "request_body", "token_case", "build", "version"]
)
def test_semantic_source_mismatch(speed_service, monkeypatch, capsys, change):
    batch, result = run_speed(speed_service, monkeypatch, capsys)
    original = tree_hashes(batch)
    copy = batch.parent / "forged"
    shutil.copytree(batch, copy)
    run = copy / "runs" / Path(result["details"]["runs"][-1]).name
    if change == "version":
        meta = json.loads((copy / "batch.json").read_text())
        meta["schema_version"] = 2
        (copy / "batch.json").write_bytes(json_bytes(meta))
    else:
        name = (
            "events.jsonl"
            if change in ("sequence", "request")
            else "request-bodies.jsonl"
            if change in ("member", "request_body")
            else "observations.jsonl"
        )
        rows = [json.loads(line) for line in (run / name).read_text().splitlines()]
        if change in ("sequence", "request"):
            event = next(
                e for e in rows if e["event_type"] == "engine_timings" and e["phase"] == "formal"
            )
            if change == "sequence":
                event["seq"] = 1
            else:
                event["request_id"] = next(
                    e["request_id"] for e in rows if e["phase"] == "probe" and e["request_id"]
                )
        elif change == "member":
            rows[-1]["request_id"] = rows[0]["request_id"]
        elif change == "request_body":
            row = rows[-1]
            row["body"]["seed"] = 42
            row["body_sha256"] = hashlib.sha256(json_bytes(row["body"])).hexdigest()
        else:
            row = next(
                r
                for r in rows
                if r["kind"]
                == ("token_budgets" if change == "token_case" else "service_properties")
            )
            if change == "token_case":
                row["payload"]["entries"][-1]["case_id"] = "context-speed-128k"
            else:
                row["payload"]["build_info"] = "unverified-build"
            row["payload_sha256"] = hashlib.sha256(json_bytes(row["payload"])).hexdigest()
        rewrite(run, name, rows)
    before, calls = tree_hashes(copy), list(speed_service["calls"])
    code, response = invoke(capsys, "speed-results", "--batch", copy)
    assert code == (2 if change == "version" else 4), response
    assert set(response["details"]) == {"schema_version", "kind", "read_state", "error"}
    assert tree_hashes(copy) == before and tree_hashes(batch) == original
    assert calls == speed_service["calls"]


@pytest.mark.parametrize("missing", ["seed", "fixed_output"])
def test_qualification_requires_observed_seed_and_fixed_output(
    speed_service, monkeypatch, capsys, missing
):
    batch, result = run_speed(speed_service, monkeypatch, capsys)
    original = tree_hashes(batch)
    copy = batch.parent / "unqualified"
    shutil.copytree(batch, copy)
    run = copy / "runs" / Path(result["details"]["runs"][-1]).name
    rows = [json.loads(line) for line in (run / "observations.jsonl").read_text().splitlines()]
    row = next(r for r in rows if r["kind"] == "effective_parameters" and r["stage"] == "stream")
    if missing == "seed":
        row["payload"]["parameters"]["seed"]["verification"] = "unverified"
    else:
        row["payload"]["slots"][0]["params"]["ignore_eos"] = False
    row["payload_sha256"] = hashlib.sha256(json_bytes(row["payload"])).hexdigest()
    rewrite(run, "observations.jsonl", rows)
    before = tree_hashes(copy)
    code, response = invoke(capsys, "speed-results", "--batch", copy)
    if missing == "seed":
        assert code == 0, response
        assert response["details"]["summary"] is None
        assert response["details"]["measurement"]["valid_count"] == 4
        assert (
            "seed_support_unverified"
            in response["details"]["samples"][-1]["measurement"]["reasons"]
        )
    else:
        assert code == 4, response
        assert response["details"]["read_state"] == "error"
    assert tree_hashes(copy) == before and tree_hashes(batch) == original
