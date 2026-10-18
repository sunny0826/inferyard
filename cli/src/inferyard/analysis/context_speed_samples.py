"""Per-request speed projection from the full ledger and physical originals."""

import hashlib

from inferyard.analysis.context_speed_cohort import cohort as measurement_cohort
from inferyard.analysis.engine_timing import engine_rates
from inferyard.analysis.input_lengths import bind_token_counts
from inferyard.evidence.context_speed import verified_trial
from inferyard.evidence.storage import EvidenceError
from inferyard.evidence.trial_evidence import verify_reference


def _value(number=None, reason="unknown", refs=()):
    return {
        "value": number,
        "missing_reason": reason if number is None else None,
        "evidence_refs": list(refs),
    }


def sample(root, trial, data):
    reads = verified_trial(data)
    prefix = data["path"].relative_to(root).as_posix()
    config, row = data["config"], data["requests"][0]
    events, _ = reads.checked_jsonl("events.jsonl")

    def ref(reference):
        if reference is None:
            return None
        verify_reference(data["path"], reference, reads=reads)
        return {**reference, "path": prefix + "/" + reference["path"]}

    def observation(kind, stage=None):
        return ref(reads.observation_reference(kind, stage=stage))

    log = ref(reads.log_reference("events.jsonl"))
    identity = reads.observation("identity") or {}
    properties = reads.observation("service_properties") or {}
    identity_refs = [r for r in (observation("identity"), observation("service_properties")) if r]
    reasons = []
    if not row["engine_build_verified"]:
        reasons.append("engine_build_unverified")
    if data["run"]["diagnostic"]:
        reasons.append("diagnostic_not_formal_measurement")
    if (
        type(row.get("completion_tokens")) is not int
        or row["completion_tokens"] != 256
        or row.get("raw_finish_reason") != "length"
        or row.get("token_source") != "endpoint.usage"
        or row.get("token_scope") != "completion_tokens"
    ):
        reasons.append("fixed_output_unverified")
    files = {f.get("path"): f.get("sha256") for f in identity.get("files", [])}
    identity_verified = identity.get("verification") == "verified" and all(
        files.get(config[section][path]) == config[section][digest]
        for section, path, digest in (
            ("model", "local_path", "sha256"),
            ("model", "template_path", "template_sha256"),
            ("engine", "binary_path", "binary_sha256"),
        )
    )
    if not identity_verified:
        reasons.append("identity_unverified")
    context = config["conditions"]["context_size"]
    context_verified = (
        row["engine_build_verified"]
        and properties.get("total_slots") == 1
        and properties.get("default_generation_settings", {}).get("n_ctx") == context
        and properties.get("model_path") == config["model"]["local_path"]
        and isinstance(properties.get("chat_template"), str)
        and hashlib.sha256(properties["chat_template"].encode()).hexdigest()
        == config["model"]["template_sha256"]
    )
    fixed_refs = []
    for stage in ("ordinary", "stream"):
        effective = reads.observation("effective_parameters", stage=stage) or {}
        item = observation("effective_parameters", stage)
        if item:
            fixed_refs.append(item)
        parameters = effective.get("parameters", {})
        for key in (
            "seed",
            "temperature",
            "top_k",
            "top_p",
            "min_p",
            "presence_penalty",
            "repeat_penalty",
            "max_tokens",
        ):
            record = parameters.get(key, {})
            if (
                record.get("verification") != "verified"
                or type(record.get("effective")) not in (int, float)
                or record["effective"] != config["generation"][key]
                or record.get("requested") != config["generation"][key]
            ):
                reasons.append(
                    "seed_support_unverified"
                    if key == "seed"
                    else "generation_parameters_unverified"
                )
    request_events = [
        e for e in events if e["request_id"] == row["request_id"] and e["phase"] == "formal"
    ]
    starts = [e for e in request_events if e["event_type"] == "request_started"]
    if len(starts) != 1 or starts[0]["data"]["case_id"] != trial["case_order"][0]:
        raise EvidenceError("context_speed_request_scope_mismatch")
    request_ref = ref(reads.request_reference(starts[0]))
    # Current layout2 supplies an authenticated request member, not a synthetic reference.
    if request_ref.get("member", {}).get("request_id") != row["request_id"]:
        raise EvidenceError("context_speed_request_member_required")
    final = [
        e for e in request_events if e["event_type"] == "engine_timings" and e["data"]["final"]
    ]
    if len(final) > 1:
        raise EvidenceError("context_speed_ambiguous_final_timing")
    timing = None
    if final:
        event = final[0]
        if events[event["seq"] - 1] != event or any(
            event[k] != data["run"][k] for k in ("run_id", "trial_id", "experiment_id")
        ):
            raise EvidenceError("context_speed_timing_binding_mismatch")
        timing = {"file": log, "event_seq": event["seq"], "record": event["data"]}
    budgets = reads.observation("token_budgets")
    token_ref = observation("token_budgets")
    counts = (
        bind_token_counts([row["case_id"]], budgets, 256) if row["engine_build_verified"] else {}
    )
    token = counts.get(row["case_id"], {})
    actual = token.get("actual_input_tokens")
    rates = engine_rates(row)
    time = None
    if timing and row["engine_build_verified"] and row["execution_state"] == "completed":
        record = timing["record"]
        if not record["missing_reason"] and not record["speculative"] and record["prompt_ms"] > 0:
            time = record["prompt_ms"] / 1000
    columns = {
        "context": _value(
            context if context_verified else None,
            "context_unverified",
            [observation("service_properties")] if context_verified else [],
        ),
        "prompt_length": _value(
            actual, "actual_input_unknown", [token_ref] if actual is not None else []
        ),
        "time": _value(time, "final_timing_unqualified", [log] if time is not None else []),
        **{
            name: _value(
                rates[code]["value"],
                rates[code]["reason"],
                [log] if rates[code]["value"] is not None else [],
            )
            for name, code in (("prefill", "L06"), ("decode", "L07"))
        },
    }
    cohort = None
    if identity_verified and context_verified and token and not reasons:
        cohort = measurement_cohort(config, identity.get("environment", {}), token)
    return {
        "trial_id": trial["trial_id"],
        "repeat_index": trial["repeat_index"],
        "case_id": row["case_id"],
        "run": {
            "path": prefix,
            "run_id": data["run"]["run_id"],
            "inputs": ref(reads.input_reference("run")),
            "manifest_sha256": reads.hashes.get("manifest.json"),
            "integrity": "sealed" if reads.manifest else "unsealed",
        },
        "request_id": row["request_id"],
        "execution_state": row["execution_state"],
        "error_category": row.get("error_category"),
        "stop_reason": _value(data["summary"]["stop_reason"], refs=[log]),
        "columns": columns,
        "proof": {
            "identity": identity_refs,
            "token_budget": token_ref,
            "request": request_ref,
            "final_timing": timing,
            "fixed_output": fixed_refs,
        },
        "measurement": {"valid": False, "reasons": reasons, "cohort_sha256": cohort},
    }
