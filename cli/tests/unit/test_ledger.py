"""Crash, lineage and five-state accounting from durable v2 source evidence."""

import copy
import json
from pathlib import Path

import pytest

from inferyard.adapters.prism import ResponseState, request_body
from inferyard.analysis.scoring import score_case
from inferyard.config.loader import load_config
from inferyard.config.planning import compile_plan
from inferyard.errors import EvidenceError
from inferyard.analysis.ledger import (
    plan_progress,
    read_trial,
    reduce_events,
    resume_selection,
)
from inferyard.evidence.storage import read_jsonl, sha256_file
from tests.legacy_journal import LegacyJournal
from tests.unit.phase2_contract_helpers import experiment

type JsonScalar = str | int | float | bool | None
type JsonNode = JsonScalar | list[JsonNode] | dict[str, JsonNode]
type JsonObject = dict[str, JsonNode]


@pytest.fixture
def setup(config_path):
    loaded = load_config(config_path)
    config = loaded.config.to_dict()
    bundle = loaded.bundle.to_dict()
    first = bundle["cases"][0]
    bundle["cases"] = [dict(copy.deepcopy(first), case_id=f"c{i}") for i in range(5)]
    source = experiment()
    workload = source["workloads"][0]
    workload["protocol"]["case_ids"] = [c["case_id"] for c in bundle["cases"]]
    workload["timeout_seconds"] = config["execution"]["timeout_seconds"]
    source["budget"].update(max_requests=100, max_wall_seconds=100_000)
    plan = compile_plan(source)
    return config, bundle, plan


def journal(tmp_path, setup, **kwargs):
    config, bundle, plan = setup
    return LegacyJournal(tmp_path, plan, plan["trials"][0]["trial_id"], config, bundle, **kwargs)


def attempt(store, setup, offset, state, *, finish="stop", score=True):
    config, bundle, _ = setup
    cid = store.selected[offset]
    case = next(c for c in bundle["cases"] if c["case_id"] == cid)
    key = f"{store.run_id}-r{offset}"
    base = 100 + offset * 100
    body = request_body(config, case["prompt"])
    store.event(
        "request_started",
        "formal",
        key,
        dict(
            case_id=cid,
            plan_index=offset,
            attempt=1,
            body=dict(
                model=body["model"],
                messages=body["messages"],
                stream=True,
                generation=config["generation"],
            ),
        ),
        monotonic_ns=base,
    )
    response = ResponseState(base + 1)
    if state == "invalid":
        return
    answer = case["reference_answer"]
    store.event("content", "formal", key, {"text": answer}, monotonic_ns=base + 2)
    response.content = [answer]
    response.t_first_content_ns = response.t_first_answer_ns = base + 2
    if state == "completed":
        response.finish_reason, response.done = finish, True
        store.event("finish", "formal", key, {"raw_finish_reason": finish}, monotonic_ns=base + 3)
        store.event("protocol_end", "formal", key, {}, monotonic_ns=base + 4)
    terminal = response.terminal(
        base + 5, None if state == "completed" else "fixture_" + state, state
    )
    store.event("request_finished", "formal", key, terminal, monotonic_ns=base + 5)
    if state == "completed" and score:
        store.event(
            "score",
            "scoring",
            key,
            score_case(case, answer, bundle["answer_policy"]),
            monotonic_ns=base + 6,
        )


def finish(store, reason="plan_finished", *, seal=True):
    store.event("run_stopped", "finalizing", None, {"reason": reason}, monotonic_ns=100_000)
    if seal:
        store.seal()
    store.close()
    return read_trial(store.path)


def _assert_pooled_failure_count(data: JsonObject) -> None:
    pooled = next(
        metric
        for metric in data["summary"]["metric_observations"]
        if metric["metric_id"] == "L05" and metric["statistic"] == "pooled_min"
    )
    assert "missing:failed:1" in pooled["limitations"]
    assert not any(
        "cancelled" in value or "invalid" in value or "not_executed" in value
        for value in pooled["limitations"]
    )


def test_five_terminal_counts_conserve_planned_cases(
    tmp_path: Path, setup: tuple[JsonObject, JsonObject, JsonObject]
) -> None:
    store = journal(tmp_path, setup)
    for i, state in enumerate(["completed", "failed", "cancelled", "invalid"]):
        attempt(store, setup, i, state)
    data = finish(store, "tool_interrupted")
    assert [r["execution_state"] for r in data["requests"]] == [
        "completed",
        "failed",
        "cancelled",
        "invalid",
        "not_executed",
    ]
    counts = data["summary"]["counts"]
    assert counts["planned"] == 5 and counts["executed"] == 4 and counts["valid_executed"] == 2
    assert data["summary"]["completeness"] == "incomplete"
    assert data["summary"]["completion_rate"]["value"] == 0.5
    _assert_pooled_failure_count(data)
    assert resume_selection(data) == ["c4"]


def test_resume_cannot_replace_failure_or_claim_continuous_complete_trial(tmp_path, setup):
    parent = journal(tmp_path, setup, run_id="parent")
    attempt(parent, setup, 0, "failed")
    attempt(parent, setup, 1, "invalid")
    before = finish(parent, "tool_interrupted")
    hashes = {p.name: sha256_file(p) for p in parent.path.iterdir()}
    child = journal(tmp_path, setup, parent=before, resume_case_ids=resume_selection(before))
    for i in range(3):
        attempt(child, setup, i, "completed")
    after = finish(child)
    assert after["run"]["parent_run_id"] == "parent"
    assert after["selection"]["parent_events_sha256"] == before["events_sha256"]
    assert after["summary"]["scope_complete"]
    assert after["summary"]["completeness"] == "incomplete"
    assert after["summary"]["quality"]["Q02"]["value"] is None
    assert hashes == {p.name: sha256_file(p) for p in parent.path.iterdir()}
    progress = plan_progress(setup[2], [before, after])
    assert progress[0]["status"] == "incomplete"
    assert len(progress[0]["runs"]) == 2
    assert progress[1]["status"] == "not_started"
    with pytest.raises(EvidenceError, match="only_unexecuted"):
        journal(tmp_path, setup, parent=before, resume_case_ids=["c0", "c2", "c3", "c4"])
    with pytest.raises(EvidenceError, match="no_unexecuted"):
        resume_selection(after)
