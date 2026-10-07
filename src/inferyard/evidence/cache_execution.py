"""Offline proof of the frozen prefix-reuse request sequence."""

from inferyard.config.cache_execution import request_value, validate_inputs
from inferyard.evidence.request_snapshots import snapshot_name_for_event
from inferyard.evidence.storage import EvidenceError, local_file, read_json


def verify_execution(root, workload, config, bundle, events, *, capabilities=None):
    if "cache_protocol" not in workload:
        return
    from inferyard.adapters.prism import request_body

    validate_inputs(workload, config)
    starts = [e for e in events if e["event_type"] == "request_started"]
    if not starts:
        return
    manifest = (
        read_json(root / "manifest.json")["files"] if (root / "manifest.json").exists() else None
    )
    if manifest is not None and "service.props.json" not in manifest:
        raise EvidenceError("cache_protocol_engine_unsealed")
    if capabilities is None:
        from inferyard.evidence.engine_capabilities import bound_capabilities

        capabilities = bound_capabilities(root, manifest)
    if not capabilities.prism_definition:
        raise EvidenceError("cache_protocol_engine_unverified")
    first = starts[0]
    if first["phase"] != "probe" or first["data"]["body"]["stream"] is not False:
        raise EvidenceError("cache_protocol_initial_probe_missing")
    ends = {e["request_id"]: e["data"] for e in events if e["event_type"] == "request_finished"}
    if len(starts) > 1 and ends.get(first["request_id"], {}).get("execution_state") != "completed":
        raise EvidenceError("cache_protocol_initial_probe_not_completed")
    for ordinal, event in enumerate(starts, 1):
        phase = (
            "probe"
            if ordinal <= 2
            else "warmup"
            if ordinal <= 2 + config["execution"]["warmup_count"]
            else "formal"
        )
        if event["phase"] != phase or event["data"]["body"]["stream"] is not (ordinal != 1):
            raise EvidenceError("cache_protocol_request_sequence_mismatch")
        if (
            phase != "formal"
            and ordinal < len(starts)
            and ends.get(event["request_id"], {}).get("execution_state") != "completed"
        ):
            raise EvidenceError("cache_protocol_continued_after_unsuccessful_request")
        name = snapshot_name_for_event(root, event, manifest)
        if manifest is not None and name not in manifest:
            raise EvidenceError("cache_protocol_request_unsealed")
        body = read_json(local_file(root, name))
        if body.get("cache_prompt") is not request_value(config, event["phase"], ordinal):
            raise EvidenceError("cache_protocol_request_policy_mismatch")
        phase = event["phase"]
        prompt = (
            next(c["prompt"] for c in bundle["cases"] if c["case_id"] == event["data"]["case_id"])
            if phase == "formal"
            else config["execution"][phase + "_prompt"]
        )
        expected = request_body(config, prompt, stream=event["data"]["body"]["stream"])
        expected["cache_prompt"] = request_value(config, phase, ordinal)
        expected["n_cache_reuse"] = 0
        if workload.get("output_mode") == "strict_fixed_length":
            expected["ignore_eos"] = True
        if body != expected:
            raise EvidenceError("cache_protocol_request_differs_from_plan")
