"""Strict structural validation and semantic dispatch for the unique wire contract."""

from __future__ import annotations

import json
import math
import re
from dataclasses import dataclass
from enum import StrEnum
from typing import Any

from inferyard import SCHEMA_VERSION
from inferyard.contracts.schemas import schemas_for

_FIELD_NAME = re.compile(r"[A-Za-z_][A-Za-z0-9_]{0,63}")
_JSON_TYPES = {
    "object": dict,
    "array": list,
    "string": str,
    "integer": int,
    "boolean": bool,
    "null": type(None),
}


class ContractError(ValueError):
    """Only field paths and fixed descriptions; never include rejected values."""

    def __init__(self, path: str, reason: str):
        self.path = path
        self.reason = reason
        super().__init__(f"{path}: {reason}")


class ExecutionState(StrEnum):
    COMPLETED = "completed"
    FAILED = "failed"
    CANCELLED = "cancelled"
    INVALID = "invalid"
    NOT_EXECUTED = "not_executed"


class QualityState(StrEnum):
    PASS = "pass"
    FAIL = "fail"
    UNSCORABLE = "unscorable"
    NOT_SCORED = "not_scored"


def _field(path: str, key: str) -> str:
    # Unknown keys can themselves contain untrusted text. Keep diagnostics bounded.
    if not _FIELD_NAME.fullmatch(key):
        return f"{path}.<unknown-field>"
    return f"{path}.{key}"


def _is_type(value: Any, kind: str) -> bool:
    value_type = type(value)
    if kind == "number":
        return value_type is int or (value_type is float and math.isfinite(value))
    return value_type is _JSON_TYPES[kind]


def _validate(value: Any, spec: dict, path: str) -> None:
    value_type = type(value)
    alternatives = spec.get("oneOf", spec.get("anyOf"))
    if alternatives is not None:
        # Select a known discriminator to keep nested errors precise.
        if value_type is dict:
            for discriminator in ("event_type", "category"):
                if discriminator not in value:
                    continue
                matches = [
                    branch
                    for branch in alternatives
                    if value[discriminator]
                    in branch.get("properties", {}).get(discriminator, {}).get("enum", [])
                ]
                if len(matches) == 1:
                    _validate(value, matches[0], path)
                    return
                if not matches:
                    raise ContractError(_field(path, discriminator), "unsupported enum value")
        successes = 0
        for branch in alternatives:
            try:
                _validate(value, branch, path)
                successes += 1
            except ContractError:
                pass
        if not successes or ("oneOf" in spec and successes != 1):
            raise ContractError(path, "does not match the allowed types or variants")
        return
    kinds = spec["type"]
    if isinstance(kinds, str):
        matches_type = _is_type(value, kinds)
    else:
        matches_type = any(_is_type(value, kind) for kind in kinds)
    if not matches_type:
        raise ContractError(path, "invalid type or non-finite number")
    if "const" in spec and value != spec["const"]:
        raise ContractError(path, "must equal the fixed contract value")
    if "enum" in spec and value not in spec["enum"]:
        raise ContractError(path, "unsupported enum value")
    if value_type in (int, float):
        # Keep the original check order and inclusive/exclusive comparisons.
        if "minimum" in spec and not value >= spec["minimum"]:
            raise ContractError(path, "violates minimum")
        if "maximum" in spec and not value <= spec["maximum"]:
            raise ContractError(path, "violates maximum")
        if "exclusiveMinimum" in spec and not value > spec["exclusiveMinimum"]:
            raise ContractError(path, "violates exclusiveMinimum")
        if "exclusiveMaximum" in spec and not value < spec["exclusiveMaximum"]:
            raise ContractError(path, "violates exclusiveMaximum")
    if value_type is str:
        if len(value) < spec.get("minLength", 0):
            raise ContractError(path, "must not be empty")
        if len(value) > spec.get("maxLength", len(value)):
            raise ContractError(path, "string exceeds maximum length")
        if "pattern" in spec and re.fullmatch(spec["pattern"], value) is None:
            raise ContractError(path, "invalid format")
    if value_type is list:
        if len(value) < spec.get("minItems", 0):
            raise ContractError(path, "too few items")
        if len(value) > spec.get("maxItems", len(value)):
            raise ContractError(path, "too many items")
        if "items" in spec:
            for index, item in enumerate(value):
                _validate(item, spec["items"], f"{path}[{index}]")
    if value_type is dict:
        if len(value) < spec.get("minProperties", 0):
            raise ContractError(path, "too few fields")
        properties = spec.get("properties", {})
        for key in spec.get("required", []):
            if key not in value:
                raise ContractError(_field(path, key), "required field missing")
        for key, item in value.items():
            if type(key) is not str:
                raise ContractError(path, "field names must be strings")
            field_path = _field(path, key)
            if key in properties:
                _validate(item, properties[key], field_path)
            elif spec.get("additionalProperties") is False:
                raise ContractError(field_path, "unknown field")
            elif isinstance(spec.get("additionalProperties"), dict):
                _validate(item, spec["additionalProperties"], field_path)


def _bundle_invariants(data: dict) -> None:
    ids = set()
    for index, case in enumerate(data["cases"]):
        path = f"bundle.cases[{index}]"
        if case["case_id"] in ids:
            raise ContractError(path + ".case_id", "duplicate case_id")
        ids.add(case["case_id"])
        rules = case["rules"]
        if case["category"] == "instruction":
            if not (
                rules["literal_contains"]
                or rules["literal_forbidden"]
                or rules["nonempty_line_count"] is not None
            ):
                raise ContractError(path + ".rules", "at least one rule required")
        else:
            for key, field in rules["fields"].items():
                if not _is_type(field["value"], field["type"]):
                    raise ContractError(
                        _field(path + ".rules.fields", key),
                        "reference value does not match declared type",
                    )


def _event_invariants(data: dict) -> None:
    kind, payload = data["event_type"], data["data"]
    if (
        kind not in ("idle_observed", "native_observed", "run_stopped")
        and data["request_id"] is None
    ):
        raise ContractError("event.request_id", "required for request events")
    if kind == "request_started" and data["phase"] == "formal":
        for key in ("case_id", "plan_index"):
            if payload[key] is None:
                raise ContractError("event.data." + key, "required for formal requests")
    if kind in ("idle_observed", "lab_usage"):
        from inferyard.contracts.lab import validate_event

        validate_event(kind, payload)
    if kind == "request_finished":
        start, end = payload["t_send_ns"], payload["t_terminal_ns"]
        if end < start or data["monotonic_ns"] < end:
            raise ContractError("event.data.t_terminal_ns", "invalid monotonic ordering")
        for key in ("t_first_content_ns", "t_first_answer_ns"):
            if payload[key] is not None and not start <= payload[key] <= end:
                raise ContractError("event.data." + key, "outside request interval")
        content, answer = payload["t_first_content_ns"], payload["t_first_answer_ns"]
        if answer is not None and (content is None or answer < content):
            raise ContractError("event.data.t_first_answer_ns", "precedes first content")
        completed = payload["execution_state"] == "completed"
        if completed and (
            not payload["protocol_complete"]
            or payload["raw_finish_reason"] not in ("stop", "length")
            or payload["error_category"] is not None
        ):
            raise ContractError(
                "event.data.execution_state", "completed requires valid termination"
            )
        if not completed and payload["error_category"] is None:
            raise ContractError("event.data.error_category", "required for abnormal termination")
        if payload["budget_exhausted"] != (payload["raw_finish_reason"] == "length"):
            raise ContractError("event.data.budget_exhausted", "inconsistent finish reason")
    if kind == "score" and payload["quality_state"] == "unscorable" and not payload["reason"]:
        raise ContractError("event.data.reason", "required for unscorable output")


def _sample_invariants(data: dict) -> None:
    if data["read_finished_ns"] < data["read_started_ns"]:
        raise ContractError("sample.read_finished_ns", "precedes read start")
    if (data["value"] is None) != (data["missing_reason"] is not None):
        raise ContractError("sample.missing_reason", "required exactly when value is missing")
    if data["metric_name"] == "service_rss" and data["value"] is not None:
        if data["server_pid"] is None or data["process_start_ticks"] is None:
            raise ContractError("sample.server_pid", "RSS value requires process identity")


def _rate_invariants(rate: dict, path: str) -> None:
    if rate["numerator"] > rate["denominator"]:
        raise ContractError(path, "numerator exceeds denominator")
    if rate["value"] is None:
        if rate["reason"] is None:
            raise ContractError(path + ".reason", "required for missing rate")
    elif (
        rate["denominator"] == 0
        or rate["reason"] is not None
        or not math.isclose(
            rate["value"], rate["numerator"] / rate["denominator"], rel_tol=1e-12, abs_tol=1e-12
        )
    ):
        raise ContractError(path + ".value", "inconsistent rate")


def validate_document(kind: str, data: Any) -> None:
    if (
        type(data) is not dict
        or type(data.get("schema_version")) is not int
        or data["schema_version"] != SCHEMA_VERSION
    ):
        raise ContractError(
            kind + ".schema_version", "unsupported schema version; explicit migration required"
        )
    schemas = schemas_for()
    if kind not in schemas:
        raise ContractError(kind, "unknown document kind")
    _validate(data, schemas[kind], kind)
    from inferyard.contracts.contracts_experiment import validate_semantics

    validate_semantics(kind, data)


def strict_json_loads(text: str) -> Any:
    def pairs(items):
        result = {}
        for key, value in items:
            if key in result:
                raise ContractError("document", "duplicate JSON key")
            result[key] = value
        return result

    def bad_constant(_):
        raise ContractError("document", "non-finite JSON number")

    def finite_float(value):
        number = float(value)
        if not math.isfinite(number):
            raise ContractError("document", "non-finite JSON number")
        return number

    try:
        return json.loads(
            text, object_pairs_hook=pairs, parse_constant=bad_constant, parse_float=finite_float
        )
    except (json.JSONDecodeError, RecursionError) as exc:
        raise ContractError("document", "invalid JSON syntax or nesting") from exc


@dataclass(frozen=True, slots=True)
class Document:
    """Detached, validated wire record; mutations of input/output cannot alter it."""

    kind: str
    _json: str

    @classmethod
    def parse(cls, kind: str, data: Any) -> Document:
        validate_document(kind, data)
        return cls(
            kind,
            json.dumps(
                data, ensure_ascii=False, allow_nan=False, sort_keys=True, separators=(",", ":")
            ),
        )

    def to_dict(self) -> dict:
        return json.loads(self._json)
