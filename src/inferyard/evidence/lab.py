"""Offline lab response, request-ID and lifecycle reconstruction for formal evidence."""

from inferyard.adapters.lab_generation import GenerationDecoder
from inferyard.adapters.lab_observation import (
    LabProtocolError,
    ObservationTracker,
    parse_identity,
)
from inferyard.adapters.lab_parameters import _equal
from inferyard.adapters.requests import request_body
from inferyard.evidence.lab_parameters import verify_parameters
from inferyard.evidence.native import observation_evidence, verify_native
from inferyard.evidence.request_snapshots import snapshot_name_for_event
from inferyard.evidence.storage import EvidenceError, json_bytes, local_file, read_json


def verify_lab(root, config, bundle, events, *, reads=None, _projection=False):
    engine = config["engine"]["adapter"]
    if engine not in ("kvmem", "ninfer"):
        if any(
            e["event_type"] in ("lab_wire", "lab_usage") or "lab_snapshot" in e["data"]
            for e in events
        ):
            raise EvidenceError("lab_evidence_requires_lab_engine")
        return False
    observation = observation_evidence(root, config)
    if observation and observation["mode"] == "native":
        if _projection:
            if any(e["event_type"] == "idle_observed" for e in events):
                raise EvidenceError("native_evidence_cannot_prove_engine_drain")
            return False
        return verify_native(root, config, bundle, events, observation)
    starts = {e["request_id"]: e for e in events if e["event_type"] == "request_started"}
    props_path = local_file(root, "service.props.json")
    if not props_path.exists():
        if starts:
            raise EvidenceError("lab_identity_evidence_missing")
        return False
    try:
        props = parse_identity(json_bytes(read_json(props_path)), expected_engine=engine)
        if (
            props["build_id"] != config["engine"]["release"]
            or props["model"]
            != {
                "kind": config["model"]["kind"],
                "sha256": config["model"]["sha256"],
                "bytes": config["model"]["bytes"],
            }
            or props["template_sha256"] != config["model"]["template_sha256"]
            or not all(props["capabilities"].values())
        ):
            raise EvidenceError("lab_identity_evidence_mismatch")
        return _verify(
            root, config, bundle, events, starts, props, reads=reads, _projection=_projection
        )
    except (LabProtocolError, KeyError, TypeError, ValueError) as exc:
        raise EvidenceError("lab_protocol_evidence_invalid") from exc


def _verify(root, config, bundle, events, starts, props, *, reads=None, _projection=False):
    instance = props["server_instance_id"]
    tracker = ObservationTracker(instance)
    cases = {case["case_id"]: case for case in bundle["cases"]}
    manifest_path = local_file(root, "manifest.json")
    manifest = read_json(manifest_path)["files"] if manifest_path.exists() else None
    decoders, bodies, usage, released, ids = {}, {}, {}, set(), set()
    for event in events:
        key, kind, data = event["request_id"], event["event_type"], event["data"]
        if kind == "request_started":
            name = snapshot_name_for_event(root, event, manifest)
            if manifest is not None and name not in manifest:
                raise EvidenceError("lab_request_unsealed")
            body = read_json(local_file(root, name))
            request_id = body.get("lab_request_id")
            if request_id in ids or body.get("lab_server_instance_id") != instance:
                raise EvidenceError("lab_request_binding_mismatch")
            ids.add(request_id)
            prompt = (
                cases[data["case_id"]]["prompt"]
                if event["phase"] == "formal"
                else config["execution"][event["phase"] + "_prompt"]
            )
            expected = {
                **request_body(config, prompt, stream=data["body"]["stream"]),
                "lab_request_id": request_id,
                "lab_server_instance_id": instance,
            }
            if "cache_policy" in body:
                # Strict-lab records sealed before release-binary support keep their wire shape.
                expected.update(
                    cache_policy="disabled", reasoning_mode=config["generation"]["reasoning_mode"]
                )
            if not _equal(body, expected):
                raise EvidenceError("lab_request_differs_from_plan")
            bodies[key] = body
            decoders[key] = GenerationDecoder(
                request_id, instance, streaming=body["stream"], t_send_ns=event["monotonic_ns"]
            )
        elif kind == "lab_wire":
            if (
                data["request_id"] != bodies[key]["lab_request_id"]
                or data["server_instance_id"] != instance
            ):
                raise EvidenceError("lab_wire_binding_mismatch")
            try:
                if decoders[key] is None:
                    continue
                decoders[key].feed(data["text"].encode("utf-8"), observed_ns=event["monotonic_ns"])
            except LabProtocolError:
                # An abnormal wire response is retained evidence, never rewritten as success.
                decoders[key] = None
        elif kind == "lab_usage":
            usage[key] = data
        elif (
            not _projection
            and kind == "request_finished"
            and data["execution_state"] == "completed"
        ):
            decoder = decoders[key]
            if decoder is None:
                raise EvidenceError("lab_completed_invalid_wire")
            result = decoder.finish(observed_ns=event["monotonic_ns"])
            if (
                any(
                    result[field] != data[field]
                    for field in ("content", "reasoning", "completion_tokens")
                )
                or result["finish_reason"] != data["raw_finish_reason"]
            ):
                raise EvidenceError("lab_wire_terminal_mismatch")
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
            if usage.get(key) != expected_usage:
                raise EvidenceError("lab_usage_evidence_mismatch")
        elif kind == "idle_observed":
            snapshot = data.get("lab_snapshot")
            if snapshot is None:
                raise EvidenceError("lab_idle_snapshot_missing")
            tracker.accept_lifecycle(snapshot["lifecycle"])
            request = snapshot["request"]
            if request is not None:
                tracker.accept_request(request)
                if key not in bodies or request["request_id"] != bodies[key]["lab_request_id"]:
                    raise EvidenceError("lab_release_binding_mismatch")
                if data["state"] == "idle":
                    released.add(key)
    finished = any(
        e["event_type"] == "run_stopped" and e["data"]["reason"] == "plan_finished" for e in events
    )
    if finished and set(starts) != released:
        raise EvidenceError("lab_finished_without_release_evidence")
    if not _projection:
        verify_parameters(root, config, props, events, bodies, usage, manifest, reads=reads)
    return manifest is not None and "service.props.json" in manifest
