"""Offline reconstruction of release-binary responses without invented lab IDs/drain."""

from inferyard.adapters.lab_observation_json import LabProtocolError
from inferyard.adapters.lab_parameters import _equal
from inferyard.adapters.native_observation import OpenAIGenerationDecoder
from inferyard.adapters.requests import request_body
from inferyard.contracts.schemas_observation import OBSERVATION
from inferyard.contracts.validation import ContractError, _validate
from inferyard.evidence.native_receipts import verify_clean_receipts
from inferyard.evidence.request_snapshots import snapshot_name_for_event
from inferyard.evidence.storage import EvidenceError, local_file, read_json


def observation_evidence(root, config):
    path = local_file(root, "engine-capabilities.json")
    if not path.exists():
        return None
    value = read_json(path)
    try:
        _validate(value, OBSERVATION, "engine_observation")
    except ContractError as exc:
        raise EvidenceError("engine_observation_invalid") from exc
    if value.get("engine") != config["engine"]["adapter"] or value.get("mode") not in (
        "lab",
        "native",
    ):
        raise EvidenceError("engine_observation_binding_mismatch")
    manifest_path = local_file(root, "manifest.json")
    if manifest_path.exists():
        manifest = read_json(manifest_path)
        if (
            "engine-capabilities.json" not in manifest["files"]
            or manifest.get("engine_observation") != value
        ):
            raise EvidenceError("engine_observation_manifest_mismatch")
    if value["mode"] == "native":
        if (
            value["engine_internal_drain"]
            != {
                "value": None,
                "scope": "capability_only",
                "missing_reason": "native_lifecycle_unavailable",
            }
            or value["request_id_scope"] != "client_only"
            or value["native_cancel"]["value"] is not None
        ):
            raise EvidenceError("native_evidence_cannot_prove_engine_drain")
        if config["engine"].get("observation_mode") == "lab_required":
            raise EvidenceError("required_lab_observation_missing")
    for record in value["endpoints"].values():
        if record["available"] is True:
            if (
                record["status_code"] != 200
                or record["missing_reason"] is not None
                or type(record["body"]) is not str
            ):
                raise EvidenceError("native_endpoint_evidence_invalid")
        elif record["available"] is not None or not record["missing_reason"]:
            raise EvidenceError("native_missing_reason_required")
    return value


def verify_native(root, config, bundle, events, observation):
    cases = {case["case_id"]: case for case in bundle["cases"]}
    decoders, usages, ids = {}, {}, set()
    manifest_path = local_file(root, "manifest.json")
    manifest = read_json(manifest_path)["files"] if manifest_path.exists() else None
    for event in events:
        key, kind, data = event["request_id"], event["event_type"], event["data"]
        if kind == "request_started":
            name = snapshot_name_for_event(root, event, manifest)
            if manifest is not None and name not in manifest:
                raise EvidenceError("native_request_unsealed")
            prompt = (
                cases[data["case_id"]]["prompt"]
                if event["phase"] == "formal"
                else config["execution"][event["phase"] + "_prompt"]
            )
            expected = request_body(config, prompt, stream=data["body"]["stream"])
            if not _equal(read_json(local_file(root, name)), expected):
                raise EvidenceError("native_request_differs_from_plan")
            decoders[key] = OpenAIGenerationDecoder(
                "0" * 32, "0" * 32, streaming=expected["stream"], t_send_ns=event["monotonic_ns"]
            )
        elif kind == "native_wire":
            identifier = data["client_request_id"]
            if key not in decoders:
                raise EvidenceError("native_wire_without_start")
            if key not in usages:
                if identifier in ids:
                    raise EvidenceError("native_client_id_reused")
                ids.add(identifier)
                usages[key] = {"id": identifier}
            if usages[key]["id"] != identifier:
                raise EvidenceError("native_wire_client_id_changed")
            try:
                if decoders[key] is not None:
                    decoders[key].feed(data["text"].encode(), observed_ns=event["monotonic_ns"])
            except LabProtocolError:
                decoders[key] = None
        elif kind == "lab_usage":
            usages.setdefault(key, {})["usage"] = data
        elif kind == "request_finished" and data["execution_state"] == "completed":
            decoder = decoders.get(key)
            if decoder is None:
                raise EvidenceError("native_completed_invalid_wire")
            try:
                result = decoder.finish(observed_ns=event["monotonic_ns"])
            except LabProtocolError as exc:
                raise EvidenceError("native_completed_invalid_wire") from exc
            if (
                any(
                    result[field] != data[field]
                    for field in ("content", "reasoning", "completion_tokens")
                )
                or result["finish_reason"] != data["raw_finish_reason"]
            ):
                raise EvidenceError("native_wire_terminal_mismatch")
            expected_usage = {
                field: result[field]
                for field in (
                    "prompt_tokens",
                    "completion_tokens",
                    "cached_tokens",
                    "reasoning_tokens",
                    "usage_missing_reasons",
                )
            }
            if usages[key].get("usage") != expected_usage:
                raise EvidenceError("native_usage_evidence_mismatch")
        elif kind == "idle_observed" or kind == "lab_wire":
            raise EvidenceError("native_evidence_cannot_prove_engine_drain")
        elif kind == "native_observed":
            if data["engine_internal_drain"] is not None:
                raise EvidenceError("native_evidence_cannot_prove_engine_drain")
    verify_clean_receipts(root, events, usages, manifest)
    return False  # Native observations never inherit lab/template/engine-timings qualification.
