"""Offline proof of the frozen fixed-output requests and probe."""

from inferyard.adapters.requests import request_body
from inferyard.config.fixed_output import enabled
from inferyard.evidence.request_snapshots import snapshot_name_for_event
from inferyard.evidence.storage import EvidenceError, local_file, read_json
from inferyard.platforms.identity import PreflightError


def verify_probe(effective, response, target):
    try:
        slots = effective["slots"]
        params = slots[0]["params"]
        known = (
            len(slots) == 1
            and params.get("ignore_eos") is True
            and type(params.get("max_tokens")) is int
            and params["max_tokens"] == target
            and response["execution_state"] == "completed"
            and response["raw_finish_reason"] == "length"
            and type(response.get("completion_tokens")) is int
            and response["completion_tokens"] == target
            and response.get("token_source") == "endpoint.usage"
            and response.get("token_scope") == "completion_tokens"
        )
    except KeyError, TypeError, IndexError:
        known = False
    if not known:
        raise PreflightError("strict_output_probe_not_verified")
    return {
        "definition": "strict_fixed_output.v1",
        "target_tokens": target,
        "ignore_eos": True,
        "source": "slots.params+probe.usage",
        "stop_scope": "empty_request_stop_not_exposed_by_slots",
    }


def verify_execution(root, workload, config, bundle, events, *, capabilities=None):
    """Rebuild request/parameter binding independently of cached summary flags."""
    strict = enabled(workload)
    starts = [e for e in events if e["event_type"] == "request_started"]
    manifest = (
        read_json(root / "manifest.json")["files"] if (root / "manifest.json").exists() else None
    )
    cases = {c["case_id"]: c for c in bundle["cases"]}
    for ordinal, event in enumerate(starts, 1):
        name = snapshot_name_for_event(root, event, manifest)
        path = local_file(root, name)
        if not strict:
            if path.exists() and read_json(path).get("ignore_eos") is True:
                raise EvidenceError("strict_output_request_without_frozen_mode")
            continue
        if manifest is not None and name not in manifest:
            raise EvidenceError("strict_output_request_unsealed")
        phase = event["phase"]
        prompt = (
            cases[event["data"]["case_id"]]["prompt"]
            if phase == "formal"
            else config["execution"][phase + "_prompt"]
        )
        expected = {
            **request_body(config, prompt, stream=event["data"]["body"]["stream"]),
            "ignore_eos": True,
        }
        if "cache_protocol" in workload:
            from inferyard.config.cache_execution import request_value

            expected["cache_prompt"] = request_value(config, phase, ordinal)
            expected["n_cache_reuse"] = 0
        if read_json(path) != expected:
            raise EvidenceError("strict_output_request_differs_from_frozen_protocol")
    if not strict or not any(e["phase"] == "formal" for e in starts):
        return
    if manifest is not None and "service.props.json" not in manifest:
        raise EvidenceError("strict_output_engine_evidence_unsealed")
    if capabilities is None:
        from inferyard.evidence.engine_capabilities import bound_capabilities

        capabilities = bound_capabilities(root, manifest)
    if not capabilities.prism_definition:
        raise EvidenceError("strict_output_engine_unverified")
    ends = {e["request_id"]: e["data"] for e in events if e["event_type"] == "request_finished"}
    for stream in (False, True):
        matching = [
            e for e in starts if e["phase"] == "probe" and e["data"]["body"]["stream"] is stream
        ]
        name = "effective-stream.json" if stream else "effective-ordinary.json"
        if len(matching) != 1 or (manifest is not None and name not in manifest):
            raise EvidenceError("strict_output_probe_evidence_missing")
        try:
            verify_probe(
                read_json(local_file(root, name)),
                ends.get(matching[0]["request_id"], {}),
                workload["output_budget_tokens"],
            )
        except PreflightError as exc:
            raise EvidenceError("strict_output_probe_evidence_invalid") from exc
