"""Strict structural validation and semantic dispatch for the unique wire contract."""

from __future__ import annotations

import json
import math
import re
from collections.abc import Callable
from dataclasses import dataclass
from enum import StrEnum
from functools import cache, lru_cache
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


@lru_cache(maxsize=1024)
def _field_suffix(key: str) -> str:
    # Unknown keys can themselves contain untrusted text. Keep diagnostics bounded.
    return "." + key if _FIELD_NAME.fullmatch(key) else ".<unknown-field>"


def _field(path: str, key: str) -> str:
    return path + _field_suffix(key)


def _is_type(value: Any, kind: str) -> bool:
    value_type = type(value)
    if kind == "number":
        return value_type is int or (value_type is float and math.isfinite(value))
    return value_type is _JSON_TYPES[kind]


def _compile(spec: dict, nodes: dict):
    """Compile schema lookups only; validation and first-error order stay unchanged."""
    identity = id(spec)
    if identity in nodes:
        return nodes[identity]
    alternatives = spec.get("oneOf", spec.get("anyOf"))
    if alternatives is not None:
        validate = _compile_union(spec, alternatives, nodes)
    else:
        validate = _compile_node(spec, nodes)
    nodes[identity] = validate
    return validate


def _compile_union(spec, alternatives, nodes):
    branches = tuple(_compile(branch, nodes) for branch in alternatives)
    discriminators = []
    for discriminator in ("event_type", "category"):
        enums = tuple(
            branch.get("properties", {}).get(discriminator, {}).get("enum", [])
            for branch in alternatives
        )
        dispatch = {}
        # Only string enums use hashing; other values retain Python membership semantics.
        if all(type(item) is str for values in enums for item in values):
            for values, branch in zip(enums, branches, strict=True):
                for item in dict.fromkeys(values):
                    dispatch.setdefault(item, []).append(branch)
        else:
            dispatch = None
        discriminators.append((discriminator, enums, dispatch))
    one_of = "oneOf" in spec

    def validate(value, path):
        if type(value) is dict:
            for discriminator, enums, dispatch in discriminators:
                if discriminator not in value:
                    continue
                selected = value[discriminator]
                if dispatch is not None and type(selected) is str:
                    matches = dispatch.get(selected, ())
                else:
                    matches = [
                        branch
                        for values, branch in zip(enums, branches, strict=True)
                        if selected in values
                    ]
                if len(matches) == 1:
                    _validate(value, matches[0], path)
                    return
                if not matches:
                    raise ContractError(_field(path, discriminator), "unsupported enum value")
        successes = 0
        for branch in branches:
            try:
                _validate(value, branch, path)
                successes += 1
            except ContractError:
                pass
        if not successes or (one_of and successes != 1):
            raise ContractError(path, "does not match the allowed types or variants")

    return validate


def _compile_node(spec, nodes):
    kinds = spec["type"]
    kinds = (kinds,) if isinstance(kinds, str) else kinds
    types = tuple(
        t for kind in kinds for t in ((int, float) if kind == "number" else (_JSON_TYPES[kind],))
    )
    has_const, const = "const" in spec, spec.get("const")
    has_enum, enum = "enum" in spec, spec.get("enum")
    minimum, maximum = spec.get("minimum"), spec.get("maximum")
    exclusive_min, exclusive_max = spec.get("exclusiveMinimum"), spec.get("exclusiveMaximum")
    min_length, max_length = spec.get("minLength", 0), spec.get("maxLength")
    pattern = re.compile(spec["pattern"]) if "pattern" in spec else None
    min_items, max_items = spec.get("minItems", 0), spec.get("maxItems")
    items = _compile(spec["items"], nodes) if "items" in spec else None
    min_properties = spec.get("minProperties", 0)
    properties = {
        key: (_compile(child, nodes), _field_suffix(key))
        for key, child in spec.get("properties", {}).items()
    }
    required = tuple((key, _field_suffix(key)) for key in spec.get("required", []))
    additional = spec.get("additionalProperties")
    if isinstance(additional, dict):
        additional = _compile(additional, nodes)

    def validate(value, path):
        value_type = type(value)
        if value_type not in types or (value_type is float and not math.isfinite(value)):
            raise ContractError(path, "invalid type or non-finite number")
        if has_const and value != const:
            raise ContractError(path, "must equal the fixed contract value")
        if has_enum and value not in enum:
            raise ContractError(path, "unsupported enum value")
        if value_type in (int, float):
            if minimum is not None and not value >= minimum:
                raise ContractError(path, "violates minimum")
            if maximum is not None and not value <= maximum:
                raise ContractError(path, "violates maximum")
            if exclusive_min is not None and not value > exclusive_min:
                raise ContractError(path, "violates exclusiveMinimum")
            if exclusive_max is not None and not value < exclusive_max:
                raise ContractError(path, "violates exclusiveMaximum")
        if value_type is str:
            if len(value) < min_length:
                raise ContractError(path, "must not be empty")
            if max_length is not None and len(value) > max_length:
                raise ContractError(path, "string exceeds maximum length")
            if pattern is not None and pattern.fullmatch(value) is None:
                raise ContractError(path, "invalid format")
        if value_type is list:
            if len(value) < min_items:
                raise ContractError(path, "too few items")
            if max_items is not None and len(value) > max_items:
                raise ContractError(path, "too many items")
            if items is not None:
                for index, item in enumerate(value):
                    _validate(item, items, f"{path}[{index}]")
        if value_type is dict:
            if len(value) < min_properties:
                raise ContractError(path, "too few fields")
            for key, suffix in required:
                if key not in value:
                    raise ContractError(path + suffix, "required field missing")
            for key, item in value.items():
                if type(key) is not str:
                    raise ContractError(path, "field names must be strings")
                child = properties.get(key)
                if child is not None:
                    _validate(item, child[0], path + child[1])
                elif additional is False:
                    raise ContractError(_field(path, key), "unknown field")
                elif callable(additional):
                    _validate(item, additional, _field(path, key))

    return validate


@cache
def _compiled_nodes():
    # Keep runtime acceleration separate from the exported schema dictionaries.
    nodes = {}
    for spec in schemas_for().values():
        _compile(spec, nodes)
    return nodes


def _validate(value: Any, spec: dict | Callable, path: str) -> None:
    if callable(spec):
        spec(value, path)
        return
    validate = _compiled_nodes().get(id(spec))
    if validate is None:
        validate = _compile(spec, {})
    validate(value, path)


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
    from inferyard.contracts.validation_cache import validation_key

    cache, key = validation_key(kind, data)
    if cache is not None and key in cache:
        return
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
    spec = schemas[kind]
    if kind == "run":
        # Identity presence selects the compatible wire variant; keep field-level
        # errors precise instead of hiding required fields behind a union error.
        spec = spec["oneOf"][int("implementation_identity" in data)]
    _validate(data, spec, kind)
    from inferyard.contracts.contracts_experiment import validate_semantics

    validate_semantics(kind, data)
    if cache is not None:
        cache.add(key)


def strict_json_loads(text: str) -> Any:
    def pairs(items):
        result = dict(items)
        if len(result) != len(items):
            raise ContractError("document", "duplicate JSON key")
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

    def __post_init__(self):
        # Direct construction must establish the same trust as parse().
        validate_document(self.kind, strict_json_loads(self._json))

    @classmethod
    def parse(cls, kind: str, data: Any) -> Document:
        validate_document(kind, data)
        document = object.__new__(cls)
        object.__setattr__(document, "kind", kind)
        object.__setattr__(
            document,
            "_json",
            json.dumps(
                data, ensure_ascii=False, allow_nan=False, sort_keys=True, separators=(",", ":")
            ),
        )
        return document

    def to_dict(self) -> dict:
        return json.loads(self._json)


def _validated_dict(kind: str, data: Any) -> dict:
    """Reuse only an immutable same-kind Document, never a mutable dict's history."""
    if type(data) is Document and data.kind == kind:
        return data.to_dict()
    validate_document(kind, data)
    return data
