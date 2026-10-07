"""Crash, lineage and five-state accounting from durable v2 source evidence."""

import copy
import json

import pytest

from inferyard.adapters.prism import ResponseState, request_body
from inferyard.analysis.scoring import score_case
from inferyard.config.loader import load_config
from inferyard.config.planning import compile_plan
from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.ledger import (
    plan_progress,
    read_trial,
    reduce_events,
    resume_selection,
)
from inferyard.evidence.storage import EvidenceError, read_jsonl, sha256_file
from tests.unit.test_phase2_contracts import experiment


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
    return TrialJournal(tmp_path, plan, plan["trials"][0]["trial_id"], config, bundle, **kwargs)


@pytest.mark.parametrize("corrupt", [None, "count", "source", "missing_event"])
def test_output_throughput_requires_matching_usage_evidence(tmp_path, setup, corrupt):
    from inferyard.evidence.storage import read_json

    store = journal(tmp_path, setup)
    attempt(store, setup, 0, "completed")
    store.close()
    events, _ = read_jsonl(store.path / "events.jsonl")
    usage = {
        **events[0],
        "event_type": "usage",
        "monotonic_ns": 102,
        "data": {
            "completion_tokens": 2,
            "prompt_tokens": 1,
            "source": "endpoint.usage",
            "scope": "completion_tokens",
        },
    }
    if corrupt != "missing_event":
        events.insert(2, usage)
    terminal = next(e["data"] for e in events if e["event_type"] == "request_finished")
    terminal.update(
        completion_tokens=3 if corrupt == "count" else 2,
        token_source="other" if corrupt == "source" else "endpoint.usage",
        token_scope="completion_tokens",
    )
    for i, event in enumerate(events, 1):
        event["seq"] = i
    args = (
        events,
        read_json(store.path / "run.json"),
        read_json(store.path / "selection.json"),
        {c["case_id"]: c for c in setup[1]["cases"]},
    )
    if corrupt:
        with pytest.raises(EvidenceError, match="terminal_usage"):
            reduce_events(*args)
    else:
        assert reduce_events(*args)[0][0]["completion_tokens"] == 2


@pytest.mark.parametrize("corruption", [None, "index", "channel", "stream", "missing_capture"])
def test_arrival_evidence_is_bound_to_terminal_and_protocol(tmp_path, setup, corruption):
    store = journal(tmp_path, setup)
    attempt(store, setup, 0, "completed")
    store.close()
    events, _ = read_jsonl(store.path / "events.jsonl")
    capture = {
        **events[0],
        "event_type": "arrival_capture",
        "monotonic_ns": 101,
        "data": {"streaming": True, "source": "decoded_delta"},
    }
    arrival = {
        **events[0],
        "event_type": "block_arrived",
        "monotonic_ns": 102,
        "data": {"index": 1, "channel": "content"},
    }
    events[1:1] = [capture, arrival]
    if corruption == "index":
        arrival["data"]["index"] = 2
    elif corruption == "channel":
        arrival["data"]["channel"] = "reasoning"
    elif corruption == "stream":
        capture["data"]["streaming"] = False
    elif corruption == "missing_capture":
        events.remove(capture)
    for i, event in enumerate(events, 1):
        event["seq"] = i
    from inferyard.evidence.storage import read_json

    args = (
        events,
        read_json(store.path / "run.json"),
        read_json(store.path / "selection.json"),
        {c["case_id"]: c for c in setup[1]["cases"]},
    )
    if corruption:
        with pytest.raises(EvidenceError, match="arrival"):
            reduce_events(*args)
    else:
        assert reduce_events(*args)[0][0]["block_arrivals"][0]["monotonic_ns"] == 102


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


def test_five_terminal_counts_conserve_planned_cases(tmp_path, setup):
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


def test_repetitions_are_new_runs_and_failed_model_requests_are_valid(tmp_path, setup):
    config, bundle, plan = setup
    parent = journal(tmp_path, setup)
    for i in range(5):
        attempt(parent, setup, i, "failed" if i == 1 else "completed", finish="length")
    first = finish(parent)
    assert first["summary"]["completeness"] == "complete"
    assert first["summary"]["counts"]["budget_exhausted"] == 4
    second = TrialJournal(
        tmp_path, plan, plan["trials"][1]["trial_id"], config, bundle, parent=first
    )
    for i in range(5):
        attempt(second, setup, i, "completed")
    data = finish(second)
    assert data["run"]["relation"] == "repeat"
    assert data["run"]["run_id"] != first["run"]["run_id"]
    assert [p["status"] for p in plan_progress(plan, [first, data])] == [
        "complete",
        "complete",
        "not_started",
    ]


def test_crash_tail_stays_invalid_and_unsealed_cannot_claim_evidence_complete(tmp_path, setup):
    store = journal(tmp_path, setup)
    attempt(store, setup, 0, "invalid")
    store.close()
    with (store.path / "events.jsonl").open("ab") as stream:
        stream.write(b'{"incomplete":')
    result = read_trial(store.path)
    assert result["requests"][0]["execution_state"] == "invalid"
    assert result["summary"]["completeness"] == "incomplete"
    assert not result["summary"]["evidence_complete"]
    assert resume_selection(result) == ["c1", "c2", "c3", "c4"]


def test_completed_without_score_is_unscorable_not_model_failure(tmp_path, setup):
    store = journal(tmp_path, setup)
    for i in range(5):
        attempt(store, setup, i, "completed", score=False)
    result = finish(store)
    assert result["summary"]["counts"]["completed"] == 5
    assert result["summary"]["counts"]["failed"] == 0
    assert result["summary"]["completeness"] == "incomplete"


@pytest.mark.parametrize(
    "mutation",
    [
        "duplicate_start",
        "overlap",
        "wrong_trial",
        "clock",
        "wrong_case",
        "seq_gap",
        "terminal_text",
        "terminal_protocol",
        "score_category",
        "after_stop",
        "score_hash",
    ],
)
def test_corrupt_event_stream_rejected_before_metrics(tmp_path, setup, mutation):
    store = journal(tmp_path, setup)
    attempt(store, setup, 0, "completed")
    finish(store)
    # Remove the seal to exercise semantic rejection independently of file hash rejection.
    (store.path / "manifest.json").unlink()
    events, _ = read_jsonl(store.path / "events.jsonl")
    if mutation in ("duplicate_start", "overlap"):
        item = copy.deepcopy(events[0])
        if mutation == "overlap":
            item["request_id"] += "-other"
        events.insert(1, item)
    elif mutation == "wrong_trial":
        events[1]["trial_id"] = "other-trial"
    elif mutation == "clock":
        events[1]["clock_id"] = "other-clock"
    elif mutation == "wrong_case":
        events[0]["data"]["case_id"] = "c4"
    elif mutation == "seq_gap":
        events.pop(1)
    elif mutation == "terminal_text":
        events[4]["data"]["content"] = "different"
    elif mutation == "terminal_protocol":
        events[2]["data"]["raw_finish_reason"] = "length"
    elif mutation == "score_category":
        events[5]["data"]["category"] = "qa"
    elif mutation == "score_hash":
        events[5]["data"]["scorer_sha256"] = "f" * 64
    elif mutation == "after_stop":
        events.append(copy.deepcopy(events[-1]))
    if mutation != "seq_gap":
        for i, event in enumerate(events, 1):
            event["seq"] = i
    (store.path / "events.jsonl").write_text("".join(json.dumps(e) + "\n" for e in events))
    with pytest.raises(EvidenceError):
        read_trial(store.path)


def test_hash_seal_rejects_original_mutation_and_run_id_cannot_be_reused(tmp_path, setup):
    store = journal(tmp_path, setup, run_id="one")
    finish(store, "preflight_blocked")
    with pytest.raises(FileExistsError):
        journal(tmp_path, setup, run_id="one")
    with (store.path / "events.jsonl").open("ab") as stream:
        stream.write(b"\n")
    with pytest.raises(EvidenceError, match="original_evidence_hash_mismatch"):
        read_trial(store.path)
