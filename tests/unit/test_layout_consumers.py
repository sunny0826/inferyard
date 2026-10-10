"""Consumer regressions from fresh public synthetic originals; no model requests."""

from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.adapters.prism import BUILD
from inferyard.adapters.requests import request_body
from inferyard.analysis.overhead_environment import environment_record
from inferyard.evidence.fixed_output import verify_execution
from inferyard.evidence.ledger import read_run_projection, read_trial
from inferyard.evidence.request_snapshots import messages_sha256, snapshot_filename
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json, read_jsonl, sha256_file
from inferyard.evidence.token_budgets import DEFINITION, planned_inputs, read_budgets
from inferyard.evidence.trial_evidence import TrialEvidence, verify_reference
from tests.evidence_layout_helpers import digest, pack_layout2, seal_layout
from tests.helpers import fixture_run


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


@pytest.mark.parametrize("collector,tokens", [(False, False), (True, False), (True, True)])
def test_old_new_reduction_conserves_scope_and_binds_real_members(tmp_path, collector, tokens):
    old, new = synthetic_pair(tmp_path, collector=collector, tokens=tokens)
    original = {path.name: path.read_bytes() for path in old.iterdir()}
    legacy_metadata, packed_metadata = {}, {}
    legacy = read_trial(old, metadata=legacy_metadata)
    packed = read_trial(new, metadata=packed_metadata)
    assert without_refs(packed) == without_refs(legacy)
    for field in ("identity", "environment_start", "service_drain"):
        assert packed_metadata[field] == legacy_metadata[field]
    assert len(list(new.iterdir())) == 8
    refs = list(all_refs(packed["summary"]))
    for reference in refs:
        assert (new / reference["path"]).is_file()
        verify_reference(new, reference)
    members = [reference["member"] for reference in refs if "member" in reference]
    assert {m["name"] for m in members if m["kind"] == "input"} == {
        "run",
        "plan",
        "selection",
        "config",
        "bundle",
    }
    evidence = TrialEvidence(new)
    assert evidence.has_observation("collector") == collector
    if collector:
        assert evidence.observation_reference("collector") in refs
    if tokens:
        assert evidence.observation_reference("token_budgets") in refs
        assert all(
            row["actual_input_tokens"] == 17 for row in packed["summary"]["input_lengths"]["bins"]
        )
    assert original == {path.name: path.read_bytes() for path in old.iterdir()}


def test_prepared_without_started_never_increases_executed(tmp_path):
    _, new = synthetic_pair(tmp_path)
    before = read_trial(new)
    rows, _ = read_jsonl(new / "request-bodies.jsonl")
    rows.append({**rows[-1], "request_id": "unsent", "ordinal": rows[-1]["ordinal"] + 1})
    (new / "request-bodies.jsonl").write_bytes(b"".join(map(json_bytes, rows)))
    seal_layout(new, before["run"])
    assert without_refs(read_trial(new)) == without_refs(before)
    assert read_run_projection(new)["summary"]["counts"] == before["summary"]["counts"]


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


def test_projection_does_not_read_prepared_or_measurement_logs(tmp_path, monkeypatch):
    _, new = synthetic_pair(tmp_path)
    original_open = Path.open

    def guarded(path, *args, **kwargs):
        assert path.name not in ("request-bodies.jsonl", "environment.jsonl", "schedule.jsonl")
        return original_open(path, *args, **kwargs)

    monkeypatch.setattr(Path, "open", guarded)
    result = read_run_projection(new)
    assert result["summary"]["counts"]["executed"] == 3
    assert "metric_observations" not in result["summary"]


def test_non_strict_mode_checks_complete_original_body(tmp_path):
    _, new = synthetic_pair(tmp_path)
    evidence = TrialEvidence(new)
    config, bundle, plan = (evidence.input(kind) for kind in ("config", "bundle", "plan"))
    rows, _ = read_jsonl(new / "request-bodies.jsonl")
    rows[0]["body"]["ignore_eos"] = True
    rows[0]["body_sha256"] = digest(rows[0]["body"])
    (new / "request-bodies.jsonl").write_bytes(b"".join(map(json_bytes, rows)))
    seal_layout(new, evidence.input("run"))
    events, _ = read_jsonl(new / "events.jsonl")
    with pytest.raises(EvidenceError, match="strict_output_request_without_frozen_mode"):
        verify_execution(new, plan["experiment"]["workloads"][0], config, bundle, events)


@pytest.mark.parametrize("damage", ["missing", "bool", "order", "extra"])
def test_token_inventory_rejects_resealed_bad_logical_members(tmp_path, damage):
    _, new = synthetic_pair(tmp_path, tokens=True)
    evidence = TrialEvidence(new)
    config, selection = evidence.input("config"), evidence.input("selection")
    rows, _ = read_jsonl(new / "observations.jsonl")
    row = next(row for row in rows if row["kind"] == "token_budgets")
    entries = row["payload"]["entries"]
    if damage == "missing":
        entries.pop()
    elif damage == "bool":
        entries[0]["phase"] = True
    elif damage == "order":
        entries.reverse()
    else:
        entries.append(deepcopy(entries[-1]))
    row["payload_sha256"] = digest(row["payload"])
    (new / "observations.jsonl").write_bytes(b"".join(map(json_bytes, rows)))
    seal_layout(new, evidence.input("run"))
    with pytest.raises(EvidenceError):
        read_budgets(new, config, selection["case_ids"])


def test_legacy_broken_seal_precedes_malformed_parsed_config(tmp_path):
    old, _ = synthetic_pair(tmp_path)
    (old / "config.frozen.json").write_bytes(b"{invalid config}\n")
    with pytest.raises(EvidenceError, match="original_evidence_hash_mismatch"):
        read_trial(old)
    with pytest.raises(EvidenceError, match="original_evidence_hash_mismatch"):
        read_run_projection(old)


def test_configuration_binding_uses_content_not_container_digest(tmp_path):
    _, new = synthetic_pair(tmp_path)
    inputs = read_json(new / "inputs.json")
    config = inputs["documents"]["config"]
    config["payload"]["model"]["display_name"] = "changed measured input"
    config["sha256"] = digest(config["payload"])
    (new / "inputs.json").write_bytes(json_bytes(inputs))
    seal_layout(new, inputs["documents"]["run"]["payload"])
    with pytest.raises((EvidenceError, ValueError)):
        read_trial(new)


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


@pytest.mark.parametrize("consumer", ["cache", "lab"])
def test_protocol_consumers_read_complete_body_and_reject_hidden_mutation(tmp_path, consumer):
    from inferyard.evidence.cache_execution import verify_execution as verify_cache
    from inferyard.evidence.lab import verify_lab

    _, new = synthetic_pair(tmp_path)
    evidence = TrialEvidence(new)
    config, bundle, plan = (evidence.input(kind) for kind in ("config", "bundle", "plan"))
    config = deepcopy(config)
    workload = deepcopy(plan["experiment"]["workloads"][0])
    rows, _ = read_jsonl(new / "request-bodies.jsonl")
    events, _ = read_jsonl(new / "events.jsonl")
    event = deepcopy(events[0])
    event["phase"] = rows[0]["phase"] = "probe"
    event["data"]["body"]["stream"] = False
    event["data"]["case_id"] = None
    if consumer == "cache":
        config["engine"]["startup_args"] = [
            "--no-cache-prompt",
            "--no-cache-idle-slots",
            "--cache-ram",
            "0",
        ]
        workload["cache_protocol"] = "prism_prefix_reuse.v1"
        props = {"build_info": BUILD}
        body = request_body(config, config["execution"]["probe_prompt"], stream=False)
        body.update(cache_prompt=False, n_cache_reuse=0)
    else:
        config["engine"]["adapter"] = "kvmem"
        config["model"].update(kind="gguf", bytes=1, sha256="c" * 64, template_sha256="d" * 64)
        props = {
            "protocol": "lab_observation.v1",
            "server_instance_id": "a" * 32,
            "engine": "kvmem",
            "build_id": config["engine"]["release"],
            "model": {key: config["model"][key] for key in ("kind", "sha256", "bytes")},
            "template_sha256": config["model"]["template_sha256"],
            "capabilities": {
                key: True
                for key in (
                    "lifecycle",
                    "request_cancel",
                    "request_id",
                    "token_budget",
                    "effective_parameters",
                )
            },
        }
        body = request_body(config, config["execution"]["probe_prompt"], stream=False)
        body.update(lab_request_id="b" * 32, lab_server_instance_id="a" * 32)
    rows[0]["body"], rows[0]["body_sha256"] = body, digest(body)
    (new / "request-bodies.jsonl").write_bytes(b"".join(map(json_bytes, rows)))
    append_observation(new, "service_properties", "post_idle", props)

    def check():
        if consumer == "cache":
            return verify_cache(new, workload, config, bundle, [event])
        return verify_lab(new, config, bundle, [event])

    check()
    body["max_tokens"] += 1
    rows[0]["body_sha256"] = digest(body)
    (new / "request-bodies.jsonl").write_bytes(b"".join(map(json_bytes, rows)))
    seal_layout(new, evidence.input("run"))
    with pytest.raises(EvidenceError, match="request_differs_from_plan"):
        check()


def test_native_manifest_claim_requires_actual_capability_member(tmp_path):
    from inferyard.evidence.native import observation_evidence

    _, new = synthetic_pair(tmp_path)
    manifest = read_json(new / "manifest.json")
    manifest["engine_observation"] = native_capability()
    (new / "manifest.json").write_bytes(json_bytes(manifest))
    config = TrialEvidence(new).input("config")
    with pytest.raises(EvidenceError, match="engine_observation_manifest_mismatch"):
        observation_evidence(new, config)


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


def test_missing_run_inputs_cannot_fall_back_to_independent_environment_sidecars(tmp_path):
    from inferyard.evidence.environment_projection import environment_reads

    (tmp_path / "environment.start.json").write_bytes(json_bytes({"platform": "Linux"}))
    (tmp_path / "events.jsonl").write_bytes(b"")
    with pytest.raises(EvidenceError, match="run_evidence_missing"):
        environment_reads(tmp_path)


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
