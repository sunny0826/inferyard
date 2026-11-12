"""Offline proof of the frozen prefix-reuse request sequence."""

from inferyard.contracts.cache_execution import request_value, validate_inputs
from inferyard.errors import EvidenceError
from inferyard.evidence.engine_capabilities import observation_sealed
from inferyard.evidence.trial_evidence import TrialEvidence


def verify_execution(
    root, workload, config, bundle, events, *, capabilities=None, reads=None, expected_bodies=None
):
    if "cache_protocol" not in workload:
        return
    from inferyard.contracts.fixed_output import enabled
    from inferyard.evidence.fixed_output import expected_body

    validate_inputs(workload, config)
    starts = [e for e in events if e["event_type"] == "request_started"]
    if not starts:
        return
    reads = reads if reads is not None else TrialEvidence(root)
    manifest = reads.manifest["files"] if reads.manifest is not None else None
    memo = {} if expected_bodies is None else expected_bodies
    cases = {c["case_id"]: c for c in bundle["cases"]}
    if manifest is not None and not observation_sealed(reads, "service_properties", manifest):
        raise EvidenceError("cache_protocol_engine_unsealed")
    if capabilities is None:
        from inferyard.evidence.engine_capabilities import bound_capabilities

        capabilities = bound_capabilities(root, manifest, reads=reads)
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
        reference = reads.request_reference(event)
        if manifest is not None and reference["path"] not in manifest:
            raise EvidenceError("cache_protocol_request_unsealed")
        body = reads.request_body(event)
        if body.get("cache_prompt") is not request_value(config, event["phase"], ordinal):
            raise EvidenceError("cache_protocol_request_policy_mismatch")
        expected = expected_body(
            workload,
            config,
            cases,
            event,
            ordinal,
            strict=enabled(workload),
            memo=memo,
        )
        if body != expected:
            raise EvidenceError("cache_protocol_request_differs_from_plan")
