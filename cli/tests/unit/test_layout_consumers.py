"""Consumer regressions from fresh public synthetic originals; no model requests."""

from copy import deepcopy

import pytest

from inferyard.adapters.prism import BUILD
from inferyard.adapters.requests import request_body
from inferyard.analysis.overhead_environment import environment_record
from inferyard.contracts.plan_math import plan_hash
from inferyard.errors import EvidenceError
from inferyard.evidence.fixed_output import verify_execution
from inferyard.analysis.ledger import read_run_projection, read_trial
from inferyard.evidence.request_snapshots import messages_sha256, snapshot_filename
from inferyard.contracts.validation import json_bytes
from inferyard.evidence.storage import read_json, read_jsonl, sha256_file
from inferyard.evidence.token_budgets import DEFINITION, planned_inputs, read_budgets
from inferyard.evidence.trial_evidence import TrialEvidence, verify_reference
from tests.evidence_layout_helpers import digest, pack_layout2, seal_layout
from tests.helpers import fixture_run
from tests.lab_service import lab_config


def bind_frozen_config(root, config):
    inputs = read_json(root / "inputs.json")
    documents = inputs["documents"]
    documents["config"]["payload"] = config
    config_hash = digest(config)
    documents["selection"]["payload"]["config_sha256"] = config_hash
    plan = documents["plan"]["payload"]
    plan["experiment"]["workloads"][0]["config"]["sha256"] = config_hash
    plan["plan_sha256"] = plan_hash(plan)
    documents["run"]["payload"]["plan_sha256"] = plan["plan_sha256"]
    for member in documents.values():
        member["sha256"] = digest(member["payload"])
    (root / "inputs.json").write_bytes(json_bytes(inputs))
    seal_layout(root, documents["run"]["payload"])


def reseal_legacy(root):
    manifest = read_json(root / "manifest.json")
    manifest["files"] = {
        path.name: {"sha256": sha256_file(path), "bytes": path.stat().st_size, "derived": False}
        for path in root.iterdir()
        if path.is_file() and path.name != "manifest.json"
    }
    (root / "manifest.json").write_bytes(json_bytes(manifest))


def synthetic_pair(tmp_path, *, collector=False, tokens=False):
    old = fixture_run(tmp_path / "old", states=["completed", "failed", "completed"])
    config = read_json(old / "config.frozen.json")
    bundle = read_json(old / "bundle.json")
    run = read_json(old / "run.json")
    events, _ = read_jsonl(old / "events.jsonl")
    renamed = {}
    for ordinal, event in enumerate((e for e in events if e["event_type"] == "request_started"), 1):
        key = f"{run['run_id']}-{event['phase']}-{ordinal}"
        renamed[event["request_id"]] = key
        prompt = next(
            c["prompt"] for c in bundle["cases"] if c["case_id"] == event["data"]["case_id"]
        )
        body = request_body(config, prompt, stream=True)
        (old / snapshot_filename(run["run_id"], event["phase"], ordinal)).write_bytes(
            json_bytes(body)
        )
        event["data"]["body"].pop("messages")
        event["data"]["body"]["messages_sha256"] = messages_sha256(body["messages"])
    for event in events:
        event["request_id"] = renamed.get(event["request_id"], event["request_id"])
    (old / "events.jsonl").write_bytes(b"".join(map(json_bytes, events)))
    samples, _ = read_jsonl(old / "memory.jsonl")
    for sample in samples:
        sample["request_id"] = renamed[sample["request_id"]]
    (old / "memory.jsonl").write_bytes(b"".join(map(json_bytes, samples)))
    if collector:
        (old / "collector.json").write_bytes(json_bytes({"collector": "synthetic-resource.v1"}))
    if tokens:
        (old / "service.props.json").write_bytes(json_bytes({"build_info": BUILD}))
        selected = read_json(old / "selection.json")["case_ids"]
        budgets = {
            "definition": DEFINITION,
            "entries": [
                {
                    "phase": phase,
                    "case_id": cid,
                    "budget": {
                        "input_tokens": 17,
                        "output_budget": config["generation"]["max_tokens"],
                        "template_prompt_sha256": "a" * 64,
                        "source": "apply-template+tokenize:add_special,parse_special",
                        "verification": "verified",
                    },
                }
                for phase, cid in planned_inputs(config, selected)
            ],
        }
        (old / "token-budgets.v2.json").write_bytes(json_bytes(budgets))
    reseal_legacy(old)
    return old, pack_layout2(old, tmp_path / "new")


def without_refs(value):
    if isinstance(value, dict):
        return {key: without_refs(item) for key, item in value.items() if key != "evidence_refs"}
    if isinstance(value, list):
        return [without_refs(item) for item in value]
    return value


def all_refs(summary):
    for observation in summary["metric_observations"]:
        yield from observation["evidence_refs"]
    yield from summary["measurement_context"]["evidence_refs"]


def test_missing_end_record_does_not_inherit_start_or_container_existence(tmp_path):
    _, new = synthetic_pair(tmp_path)
    rows, _ = read_jsonl(new / "observations.jsonl")
    rows = [row for row in rows if (row["kind"], row["stage"]) != ("environment", "end")]
    for seq, row in enumerate(rows, 1):
        row["seq"] = seq
    (new / "observations.jsonl").write_bytes(b"".join(map(json_bytes, rows)))
    seal_layout(new, TrialEvidence(new).input("run"))
    data = read_trial(new)
    context = data["summary"]["measurement_context"]
    assert "environment_endpoints_missing" in context["environment"]["reasons"]
    assert not context["environment_qualification"]["eligible"]
    record = environment_record(new, data)
    assert record["endpoints"][1] == {}
    assert record["endpoint_reasons"] == [
        "environment_endpoint_unsealed_or_missing:environment.end.json"
    ]


def append_observation(root, kind, stage, payload, *, key=None):
    evidence = TrialEvidence(root)
    rows, _ = read_jsonl(root / "observations.jsonl")
    run = evidence.input("run")
    rows.append(
        {
            "schema_version": 3,
            "evidence_layout_version": 2,
            "run_id": run["run_id"],
            "trial_id": run["trial_id"],
            "seq": len(rows) + 1,
            "kind": kind,
            "stage": stage,
            "key": key,
            "clock_id": "fixture-clock",
            "monotonic_ns": len(rows),
            "utc": "2026-10-10T00:00:00Z",
            "payload": payload,
            "payload_sha256": digest(payload),
        }
    )
    (root / "observations.jsonl").write_bytes(b"".join(map(json_bytes, rows)))
    seal_layout(root, run)


def native_capability():
    from inferyard.contracts.schemas_observation import PATHS

    return {
        "mode": "native",
        "engine": "kvmem",
        "generation": "/v1/chat/completions",
        "endpoints": {
            path: {
                "available": None,
                "missing_reason": "http_404",
                "status_code": 404,
                "body": None,
            }
            for path in PATHS
        },
        "request_id_scope": "client_only",
        "engine_internal_drain": {
            "value": None,
            "scope": "capability_only",
            "missing_reason": "native_lifecycle_unavailable",
        },
        "native_cancel": {"value": None, "missing_reason": "native_cancel_unavailable"},
        "process_evidence": "synthetic process identity",
    }


@pytest.mark.parametrize("damage", [None, "missing", "unexpected", "scope", "client_id", "drain"])
def test_native_receipts_bind_inventory_and_never_prove_engine_drain(tmp_path, damage):
    from inferyard.evidence.native_receipts import verify_clean_receipts
    from inferyard.evidence.service_drain import service_drain

    _, new = synthetic_pair(tmp_path)
    events, _ = read_jsonl(new / "events.jsonl")
    completed = [
        e
        for e in events
        if e["event_type"] == "request_finished" and e["data"]["execution_state"] == "completed"
    ]
    clients = {e["request_id"]: {"id": str(index) * 32} for index, e in enumerate(completed, 1)}
    for index, event in enumerate(completed):
        if damage == "missing" and index == 0:
            continue
        key = event["request_id"]
        receipt = {
            "run_id": event["run_id"],
            "request_id": key,
            "client_request_id": clients[key]["id"],
            "dirty_token": "a" * 32,
            "completion_scope": "client_http",
            "basis": "validated_http_response_and_closed_context",
            "finish_reason": event["data"]["raw_finish_reason"],
            "engine_internal_drain": None,
            "missing_reason": "native_lifecycle_unavailable",
        }
        if damage == "unexpected" and index == 0:
            key = receipt["request_id"] = "unexpected"
        elif damage in ("scope", "client_id", "drain") and index == 0:
            receipt[
                {
                    "scope": "completion_scope",
                    "client_id": "client_request_id",
                    "drain": "engine_internal_drain",
                }[damage]
            ] = True if damage == "drain" else "wrong"
        append_observation(new, "native_clean_receipt", "client_complete", receipt, key=key)
    evidence = TrialEvidence(new)
    if damage:
        with pytest.raises(EvidenceError, match="native_clean_receipt"):
            verify_clean_receipts(new, events, clients, evidence.manifest["files"], reads=evidence)
    else:
        verify_clean_receipts(new, events, clients, evidence.manifest["files"], reads=evidence)
        for event in completed:
            verify_reference(
                new, evidence.observation_reference("native_clean_receipt", key=event["request_id"])
            )
        assert (
            service_drain(events, sealed=True, truncated=False, observation=native_capability())
            is None
        )


@pytest.mark.parametrize("filename", ["observations.jsonl", "request-bodies.jsonl"])
@pytest.mark.parametrize("sealed", [False, True])
def test_container_tail_retains_denominator_and_rejects_sealed_run(tmp_path, filename, sealed):
    _, new = synthetic_pair(tmp_path)
    before = read_trial(new)
    path = new / filename
    tail_offset = path.stat().st_size
    with path.open("ab") as stream:
        stream.write(b'{"unfinished":')
    if sealed:
        seal_layout(new, before["run"])
        with pytest.raises(EvidenceError, match="sealed_jsonl_truncated"):
            read_trial(new)
    else:
        (new / "manifest.json").unlink()
        result = read_trial(new)
        assert result["summary"]["counts"] == before["summary"]["counts"]
        assert result["summary"]["completeness"] == "incomplete"
        assert result["summary"]["scope_complete"] is False
        assert result["summary"]["evidence_complete"] is False
        assert (
            f"{filename}:truncated_tail_at_byte:{tail_offset}" in result["summary"]["limitations"]
        )
