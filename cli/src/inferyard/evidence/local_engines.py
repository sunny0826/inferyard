"""Recompute local queue evidence against sealed process identity, without live IO."""

from pathlib import Path

from inferyard.adapters.engine_fit import FitTransportError, _json
from inferyard.adapters.lab_parameters import _equal
from inferyard.adapters.local_engines.protocol import VERSION_PATHS, models, queue, service
from inferyard.evidence.storage import EvidenceError
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.platforms.identity import PreflightError
from inferyard.platforms.local_engines import validate_frozen_identity
from inferyard.platforms.local_engines.service import validate_binding

_KEYS = {
    "definition",
    "family",
    "served_model",
    "binding",
    "source",
    "raw_metrics",
    "observation",
    "client_uncertain",
    "missing_reason",
}


def _sources(runtime):
    expected = {
        "local_engine_source_sha256": "data/local_engine/mlx_server.py",
        "generation_queue_source_sha256": "data/engine_fit/mlx_server.py",
    }
    result = {}
    for key, suffix in expected.items():
        matches = []
        for root in runtime["roots"]:
            for entry in root["entries"]:
                path = Path(root["path"])
                if path.name != entry["path"]:
                    path /= entry["path"]
                if str(path).endswith("/" + suffix):
                    matches.append(entry["sha256"])
        if len(matches) != 1:
            raise EvidenceError("local_engine_source_evidence_invalid")
        result[key] = matches[0]
    return result


def _bound(reads, config):
    identity = reads.observation("identity")
    local = identity["local_engine"]
    validate_frozen_identity(config, local, files=identity["files"])
    binding = local["binding"]
    validate_binding(binding, config)
    capability = reads.observation("engine_capabilities")
    if (
        capability["definition"] != "local-engine-observation.v1"
        or capability["family"] != config["engine"]["family"]
        or capability["mode"] != "native"
    ):
        raise EvidenceError("local_engine_capability_binding_mismatch")
    endpoints = capability["endpoints"]
    models(endpoints["/v1/models"]["body"], config["engine"]["served_model"])
    family = config["engine"]["family"]
    props = reads.observation("service_properties")
    if family == "lmstudio":
        queue(family, config["engine"]["served_model"], None, _json(endpoints["lms:ps"]["body"]))
    payload = (
        props["service"]
        if family == "lmstudio"
        else _json(endpoints[VERSION_PATHS[family]]["body"])
    )
    verified = service(
        family,
        payload,
        config,
        sources=_sources(local["runtime"]) if family == "mlx-lm" else None,
    )
    if not _equal(props, {"service": verified, "observation": capability}):
        raise EvidenceError("local_engine_service_evidence_mismatch")
    return binding["service"]


def _snapshot(data, config, binding):
    if (
        data.get("engine_internal_drain") is not None
        or data.get("scope") != "native_endpoints"
        or data.get("missing_reason") != "native_lifecycle_unavailable"
    ):
        raise EvidenceError("native_evidence_cannot_prove_engine_drain")
    snapshot = data["signals"]["queue_snapshot"]
    family = config["engine"]["family"]
    if (
        set(snapshot) != _KEYS
        or snapshot["definition"] != "local-engine-queue.v1"
        or snapshot["family"] != family
        or snapshot["served_model"] != config["engine"]["served_model"]
        or not _equal(snapshot["binding"], binding)
        or snapshot["source"] != ("lms:ps" if family == "lmstudio" else "/metrics")
        or type(snapshot["client_uncertain"]) is not bool
    ):
        raise EvidenceError("local_engine_queue_binding_mismatch")
    observed = snapshot["observation"]
    if observed is None:
        if (
            snapshot["missing_reason"] != "local_engine_queue_unverified"
            or snapshot["client_uncertain"] is not True
        ):
            raise EvidenceError("local_engine_queue_missing_reason")
        return False, True
    if snapshot["missing_reason"] is not None:
        raise EvidenceError("local_engine_queue_missing_reason")
    parsed = queue(family, snapshot["served_model"], snapshot["raw_metrics"], observed)
    if not _equal(parsed, observed):
        raise EvidenceError("local_engine_queue_reconstruction_mismatch")
    if family != "lmstudio":
        endpoint = data["signals"]["/metrics"]
        if (
            endpoint["available"] is not True
            or endpoint["status_code"] != 200
            or endpoint["missing_reason"] is not None
            or endpoint["body"] != snapshot["raw_metrics"]
        ):
            raise EvidenceError("local_engine_queue_source_mismatch")
    uncertain = snapshot["client_uncertain"]
    return parsed["idle"] and not uncertain, uncertain


def verify_queue_events(root, config, events, reads=None):
    reads = reads if reads is not None else TrialEvidence(root)
    if config["engine"]["adapter"] != "local_openai_v1":
        raise EvidenceError("local_engine_profile_required")
    try:
        binding = _bound(reads, config)
        receipts = set(reads.observation_keys("native_clean_receipt", stage="client_complete"))
        ready, poisoned, active, last = False, False, None, None
        completed, post_ready = set(), set()
        finished = False
        for event in events:
            kind, key, data = event["event_type"], event["request_id"], event["data"]
            if kind == "native_observed":
                observed_ready, uncertain = _snapshot(data, config, binding)
                if (poisoned or active is not None) and not uncertain:
                    raise EvidenceError("local_engine_uncertainty_erased")
                poisoned |= uncertain
                if key is None:
                    ready = observed_ready
                elif key != last or active is not None:
                    raise EvidenceError("local_engine_queue_request_mismatch")
                elif observed_ready:
                    if key not in completed:
                        raise EvidenceError("local_engine_failed_request_cleaned")
                    post_ready.add(key)
            elif kind == "request_started":
                if not ready or poisoned or active is not None:
                    raise EvidenceError("local_engine_request_without_queue_admission")
                if last is not None and (last not in post_ready or last not in receipts):
                    raise EvidenceError("local_engine_request_before_clean_receipt")
                ready, active = False, key
            elif kind == "request_finished":
                if active != key:
                    raise EvidenceError("local_engine_queue_request_mismatch")
                active, last = None, key
                if data["execution_state"] == "completed":
                    completed.add(key)
                else:
                    poisoned = True
            elif kind == "idle_observed" or kind == "lab_wire":
                raise EvidenceError("native_evidence_cannot_prove_engine_drain")
            elif kind == "run_stopped" and data["reason"] == "plan_finished":
                finished = True
        if receipts != post_ready or (finished and (completed != post_ready or active is not None)):
            raise EvidenceError("local_engine_queue_receipt_mismatch")
    except (KeyError, TypeError, ValueError, FitTransportError, PreflightError) as exc:
        raise EvidenceError("local_engine_queue_evidence_invalid") from exc
    return False
