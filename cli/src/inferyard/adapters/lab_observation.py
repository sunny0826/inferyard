"""Strict parsers for the frozen lab_observation.v1 observation protocol (T0-A, ADR030).

Pure parsing and validation only: no HTTP, no process or file handling, no ledger writes.
The /lab/v1/* routes are a project interface pending an engine patch; release binaries are
not verified to provide them, and simulated payloads must not fall back to /slots or /monitor.
"""

from __future__ import annotations

from copy import deepcopy
from typing import Any

from inferyard.adapters.lab_observation_json import (
    ACTIVE_PHASES,
    ENGINES,
    MODEL_KINDS,
    PHASE_RANK,
    PROTOCOL,
    RANK_RELEASED,
    LabProtocolError,
    load_strict,
    require_bool,
    require_exact_keys,
    require_id32,
    require_int,
    require_object,
    require_sha256,
    require_str_enum,
)

__all__ = [
    "LabProtocolError",
    "ObservationTracker",
    "is_idle",
    "parse_cancel",
    "parse_identity",
    "parse_lifecycle",
    "parse_request",
]

IDENTITY_REASON = "lab_invalid_identity"
LIFECYCLE_REASON = "lab_invalid_lifecycle"
REQUEST_REASON = "lab_invalid_request"
CANCEL_REASON = "lab_invalid_cancel"
INSTANCE_REASON = "lab_instance_mismatch"
REQUEST_ID_REASON = "lab_request_mismatch"
REGRESSED_REASON = "lab_observation_regressed"

_IDENTITY_KEYS = frozenset(
    {
        "protocol",
        "server_instance_id",
        "engine",
        "build_id",
        "model",
        "template_sha256",
        "capabilities",
    }
)
_CAPABILITY_KEYS = frozenset(
    {"lifecycle", "request_cancel", "request_id", "token_budget", "effective_parameters"}
)
_LIFECYCLE_KEYS = frozenset(
    {
        "protocol",
        "server_instance_id",
        "snapshot_seq",
        "poisoned",
        "active_total",
        "stages",
        "requests",
    }
)
_REQUEST_ITEM_KEYS = frozenset({"request_id", "phase", "cancel_requested"})
_REQUEST_KEYS = frozenset(
    {
        "protocol",
        "server_instance_id",
        "snapshot_seq",
        "request_id",
        "phase",
        "cancel_requested",
        "outcome",
    }
)
_CANCEL_KEYS = frozenset({"protocol", "server_instance_id", "request_id", "disposition"})
_CANCEL_DISPOSITIONS = frozenset({"accepted", "already_released"})
_CANCEL_ERRORS = frozenset(
    {
        "instance_mismatch",
        "duplicate_request_id",
        "request_not_found",
        "request_expired",
        "request_registry_full",
    }
)
_OUTCOMES = frozenset({"completed", "failed", "cancelled"})
_MAX_ACTIVE_REQUESTS = 16


def parse_identity(raw: bytes, *, expected_engine: str) -> dict:
    if expected_engine not in ENGINES:
        raise LabProtocolError(IDENTITY_REASON)
    data = require_object(load_strict(raw, IDENTITY_REASON), IDENTITY_REASON)
    require_exact_keys(data, _IDENTITY_KEYS, IDENTITY_REASON)
    if data["protocol"] != PROTOCOL or data["engine"] != expected_engine:
        raise LabProtocolError(IDENTITY_REASON)
    require_id32(data["server_instance_id"], IDENTITY_REASON)
    build_id = data["build_id"]
    if type(build_id) is not str or not 1 <= len(build_id) <= 128:
        raise LabProtocolError(IDENTITY_REASON)
    if any(ord(char) < 0x20 or ord(char) > 0x7E for char in build_id):
        raise LabProtocolError(IDENTITY_REASON)
    model = require_object(data["model"], IDENTITY_REASON)
    require_exact_keys(model, {"kind", "sha256", "bytes"}, IDENTITY_REASON)
    if model["kind"] != MODEL_KINDS[expected_engine]:
        raise LabProtocolError(IDENTITY_REASON)
    require_sha256(model["sha256"], IDENTITY_REASON)
    require_int(model["bytes"], IDENTITY_REASON, minimum=1)
    template = data["template_sha256"]
    if template is not None:
        require_sha256(template, IDENTITY_REASON)
    capabilities = require_object(data["capabilities"], IDENTITY_REASON)
    require_exact_keys(capabilities, _CAPABILITY_KEYS, IDENTITY_REASON)
    for value in capabilities.values():
        require_bool(value, IDENTITY_REASON)
    return data


def _validate_lifecycle(data: Any) -> dict:
    data = require_object(data, LIFECYCLE_REASON)
    require_exact_keys(data, _LIFECYCLE_KEYS, LIFECYCLE_REASON)
    if data["protocol"] != PROTOCOL:
        raise LabProtocolError(LIFECYCLE_REASON)
    require_id32(data["server_instance_id"], LIFECYCLE_REASON)
    require_int(data["snapshot_seq"], LIFECYCLE_REASON, minimum=1)
    require_bool(data["poisoned"], LIFECYCLE_REASON)
    active_total = require_int(data["active_total"], LIFECYCLE_REASON)
    stages = require_object(data["stages"], LIFECYCLE_REASON)
    require_exact_keys(stages, ACTIVE_PHASES, LIFECYCLE_REASON)
    for count in stages.values():
        require_int(count, LIFECYCLE_REASON)
    requests = data["requests"]
    if type(requests) is not list or len(requests) > _MAX_ACTIVE_REQUESTS:
        raise LabProtocolError(LIFECYCLE_REASON)
    seen_ids = set()
    tally = dict.fromkeys(ACTIVE_PHASES, 0)
    for item in requests:
        item = require_object(item, LIFECYCLE_REASON)
        require_exact_keys(item, _REQUEST_ITEM_KEYS, LIFECYCLE_REASON)
        request_id = require_id32(item["request_id"], LIFECYCLE_REASON)
        if request_id in seen_ids:
            raise LabProtocolError(LIFECYCLE_REASON)
        seen_ids.add(request_id)
        phase = item["phase"]
        require_str_enum(phase, ACTIVE_PHASES, LIFECYCLE_REASON)
        tally[phase] += 1
        require_bool(item["cancel_requested"], LIFECYCLE_REASON)
    if active_total != len(requests) or any(
        stages[phase] != tally[phase] for phase in ACTIVE_PHASES
    ):
        raise LabProtocolError(LIFECYCLE_REASON)
    return data


def parse_lifecycle(raw: bytes, *, expected_instance_id: str) -> dict:
    data = _validate_lifecycle(load_strict(raw, LIFECYCLE_REASON))
    if data["server_instance_id"] != expected_instance_id:
        raise LabProtocolError(INSTANCE_REASON)
    return data


def _validate_request(data: Any) -> dict:
    data = require_object(data, REQUEST_REASON)
    require_exact_keys(data, _REQUEST_KEYS, REQUEST_REASON)
    if data["protocol"] != PROTOCOL:
        raise LabProtocolError(REQUEST_REASON)
    require_id32(data["server_instance_id"], REQUEST_REASON)
    require_id32(data["request_id"], REQUEST_REASON)
    require_int(data["snapshot_seq"], REQUEST_REASON, minimum=1)
    phase = data["phase"]
    outcome = data["outcome"]
    require_bool(data["cancel_requested"], REQUEST_REASON)
    if phase == "released":
        require_str_enum(outcome, _OUTCOMES, REQUEST_REASON)
    else:
        require_str_enum(phase, ACTIVE_PHASES, REQUEST_REASON)
        if outcome is not None:
            raise LabProtocolError(REQUEST_REASON)
    return data


def parse_request(raw: bytes, *, expected_instance_id: str, expected_request_id: str) -> dict:
    data = _validate_request(load_strict(raw, REQUEST_REASON))
    if data["server_instance_id"] != expected_instance_id:
        raise LabProtocolError(INSTANCE_REASON)
    if data["request_id"] != expected_request_id:
        raise LabProtocolError(REQUEST_ID_REASON)
    return data


def parse_cancel(raw: bytes, *, expected_instance_id: str, expected_request_id: str) -> dict:
    data = require_object(load_strict(raw, CANCEL_REASON), CANCEL_REASON)
    if set(data) == {"protocol", "error"}:
        if data["protocol"] != PROTOCOL:
            raise LabProtocolError(CANCEL_REASON)
        require_str_enum(data["error"], _CANCEL_ERRORS, CANCEL_REASON)
        return data
    require_exact_keys(data, _CANCEL_KEYS, CANCEL_REASON)
    if data["protocol"] != PROTOCOL:
        raise LabProtocolError(CANCEL_REASON)
    require_id32(data["server_instance_id"], CANCEL_REASON)
    require_id32(data["request_id"], CANCEL_REASON)
    if data["server_instance_id"] != expected_instance_id:
        raise LabProtocolError(INSTANCE_REASON)
    if data["request_id"] != expected_request_id:
        raise LabProtocolError(REQUEST_ID_REASON)
    require_str_enum(data["disposition"], _CANCEL_DISPOSITIONS, CANCEL_REASON)
    return data


def is_idle(snapshot: dict) -> bool:
    data = _validate_lifecycle(snapshot)
    return data["active_total"] == 0 and data["poisoned"] is False


class ObservationTracker:
    """Per-instance regression checks over lifecycle and request snapshots.

    Lifecycle snapshots form one sequence stream; each request_id forms its own stream.
    Identical re-observation at the same snapshot_seq is allowed; conflicting content,
    sequence regression, phase rollback, cancel-flag rollback, and revival of a released
    request are rejected.
    """

    def __init__(self, instance_id: str):
        self._instance_id = require_id32(instance_id, INSTANCE_REASON)
        self._lifecycle_seq: int | None = None
        self._lifecycle_snapshot: dict | None = None
        self._request_seqs: dict[str, int] = {}
        self._request_snapshots: dict[str, dict] = {}
        self._request_states: dict[str, list] = {}

    def accept_lifecycle(self, snapshot: dict) -> None:
        data = _validate_lifecycle(snapshot)
        self._check_instance(data)
        seq = data["snapshot_seq"]
        if self._lifecycle_seq is not None:
            if seq < self._lifecycle_seq:
                raise LabProtocolError(REGRESSED_REASON)
            if seq == self._lifecycle_seq and data != self._lifecycle_snapshot:
                raise LabProtocolError(REGRESSED_REASON)
        for item in data["requests"]:
            self._advance(item["request_id"], PHASE_RANK[item["phase"]], item["cancel_requested"])
        if seq != self._lifecycle_seq:
            self._lifecycle_seq = seq
            self._lifecycle_snapshot = deepcopy(data)

    def accept_request(self, snapshot: dict) -> None:
        data = _validate_request(snapshot)
        self._check_instance(data)
        request_id = data["request_id"]
        seq = data["snapshot_seq"]
        previous_seq = self._request_seqs.get(request_id)
        if previous_seq is not None:
            if seq < previous_seq:
                raise LabProtocolError(REGRESSED_REASON)
            if seq == previous_seq and data != self._request_snapshots[request_id]:
                raise LabProtocolError(REGRESSED_REASON)
        rank = RANK_RELEASED if data["phase"] == "released" else PHASE_RANK[data["phase"]]
        self._advance(request_id, rank, data["cancel_requested"])
        if seq != previous_seq:
            self._request_seqs[request_id] = seq
            self._request_snapshots[request_id] = deepcopy(data)

    def _check_instance(self, data: dict) -> None:
        if data["server_instance_id"] != self._instance_id:
            raise LabProtocolError(INSTANCE_REASON)

    def _advance(self, request_id: str, rank: int, cancel_requested: bool) -> None:
        state = self._request_states.get(request_id)
        if state is None:
            self._request_states[request_id] = [rank, cancel_requested]
            return
        if rank < state[0] or (state[1] and not cancel_requested):
            raise LabProtocolError(REGRESSED_REASON)
        state[0] = rank
        state[1] = cancel_requested
