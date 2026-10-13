"""Bind the control summary to actual normal trials and preserved baseline responses."""

import hashlib

from inferyard.contracts.validation import validate_document
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import (
    EvidenceError,
    json_bytes,
    local_file,
    read_json,
    read_jsonl,
    sha256_file,
)
from inferyard.runtime.overhead_runner import request_projection


def verify_control_rows(root, packet, *, verified_trials=None):
    spec, guard = packet["spec"], packet["guard"]
    trial_plan = read_json(local_file(root, "measurement-plan.json"))
    validate_document("plan", trial_plan)
    if trial_plan["plan_sha256"] != spec["trial_plan_sha256"]:
        raise EvidenceError("total_control_saved_plan_mismatch")
    samples, issues = read_jsonl(local_file(root, "guardian.jsonl"))
    if issues or samples != guard["samples"]:
        raise EvidenceError("total_control_guard_raw_samples_mismatch")
    children = read_json(local_file(root, "child-evidence.json"))
    expected_children = []
    for arm in packet["rows"]:
        if arm["mode"] == "on":
            path = local_file(root, arm["trial_path"])
            if sha256_file(path / "manifest.json") != arm["trial_manifest_sha256"]:
                raise EvidenceError("total_control_trial_manifest_mismatch")
            metadata = {}
            data = read_trial(path, metadata=metadata)
            if (
                data["plan"] != trial_plan
                or data["run"].get("tool_source_sha256") != spec["tool_source_sha256"]
            ):
                raise EvidenceError("total_control_trial_source_or_plan_mismatch")
            if verified_trials is not None:
                verified_trials[arm["trial_path"]] = (data, metadata)
            rows = data["requests"]
            inputs = data
            expected_children.append(
                {"path": arm["trial_path"], "manifest_sha256": arm["trial_manifest_sha256"]}
            )
        else:
            baseline = read_json(local_file(root, arm["baseline_path"]))
            if (
                baseline.get("definition") != "trial_control_baseline.v1"
                or baseline["snapshots"].get("plan.json") != trial_plan
                or baseline["clock_id"] != guard["clock_id"]
            ):
                raise EvidenceError("total_control_baseline_binding_mismatch")
            starts = {
                e["request_id"]: e["data"]
                for e in baseline["events"]
                if e["phase"] == "formal" and e["event_type"] == "request_started"
            }
            rows = [
                {**e["data"], "case_id": starts[e["request_id"]]["case_id"]}
                for e in baseline["events"]
                if e["phase"] == "formal" and e["event_type"] == "request_finished"
            ]
            if rows != baseline["requests"]:
                raise EvidenceError("total_control_baseline_raw_requests_mismatch")
            inputs = {
                "config": baseline["snapshots"]["config.frozen.json"],
                "bundle": baseline["snapshots"]["bundle.json"],
            }
        if any(
            hashlib.sha256(json_bytes(inputs[key])).hexdigest() != spec[key + "_sha256"]
            for key in ("config", "bundle")
        ):
            raise EvidenceError("total_control_arm_frozen_inputs_mismatch")
        if any(
            type(r.get("t_send_ns")) is not int
            or type(r.get("t_terminal_ns")) is not int
            or not arm["begin_ns"] <= r["t_send_ns"] < r["t_terminal_ns"] <= arm["after_close_ns"]
            for r in rows
        ):
            raise EvidenceError("total_control_request_clock_outside_arm")
        projected = [request_projection(row) for row in rows]
        expected = [
            {
                "case_id": r["case_id"],
                "state": r["execution_state"],
                "duration_ns": r["duration_ns"],
                "output_sha256": r["output_identity"],
            }
            for r in projected
        ]
        if expected != arm["requests"]:
            raise EvidenceError("total_control_arm_requests_do_not_replay")
    if sorted(children, key=lambda r: r["path"]) != sorted(
        expected_children, key=lambda r: r["path"]
    ):
        raise EvidenceError("total_control_child_inventory_mismatch")
