"""Unified corpus validation and hash-bound, explicit human review."""

import hashlib
import json
import math
import re

from jsonschema import Draft202012Validator
from jsonschema.exceptions import SchemaError

from inferyard.contracts.schemas_tasks import BUNDLE, CASE
from inferyard.contracts.validation import ContractError, Document, _bundle_invariants, _validate


def content_hash(bundle):
    return hashlib.sha256(
        json.dumps(
            {k: v for k, v in bundle.items() if k != "review_records"},
            ensure_ascii=False,
            sort_keys=True,
            separators=(",", ":"),
            allow_nan=False,
        ).encode()
    ).hexdigest()


def pointer(value, path):
    """Resolve RFC6901 paths without permissive array indices or fallback values."""
    if not re.fullmatch(r"(?:/(?:[^~/]|~[01])*)*", path):
        raise ContractError("case.rules.fields", "invalid JSON pointer")
    current = value
    for part in path.split("/")[1:]:
        key = part.replace("~1", "/").replace("~0", "~")
        if type(current) is dict:
            current = current[key]
        elif type(current) is list and re.fullmatch(r"0|[1-9][0-9]*", key):
            current = current[int(key)]
        else:
            raise KeyError(path)
    return current


def _json_tree(value, *, schema=False, depth=0):
    if depth > 64:
        raise ContractError("case.rules", "nesting limit exceeded")
    if type(value) is dict:
        for key, child in value.items():
            if type(key) is not str:
                raise ContractError("case.rules", "JSON object requires string keys")
            if schema and key in ("$ref", "$dynamicRef", "$recursiveRef"):
                raise ContractError("case.rules.json_schema", "references are not supported")
            _json_tree(child, schema=schema, depth=depth + 1)
    elif type(value) is list:
        for child in value:
            _json_tree(child, schema=schema, depth=depth + 1)
    elif type(value) is float and not math.isfinite(value):
        raise ContractError("case.rules", "non-finite JSON value")
    elif value is not None and type(value) not in (str, int, float, bool):
        raise ContractError("case.rules", "non-JSON value")


def validate_case(case):
    _validate(case, CASE, "case")
    _validate_case_semantics(case)


def _validate_case_semantics(case):
    category, rules = case["category"], case["rules"]
    if category == "svg":
        # CASE requires reference_answer and an empty rules object; no quality rules apply.
        return
    if category in ("instruction", "extraction"):
        _bundle_invariants({"cases": [case]})
    elif category == "classification":
        if (
            len(set(rules["labels"])) != len(rules["labels"])
            or rules["expected"] not in rules["labels"]
        ):
            raise ContractError("case.rules.labels", "duplicate label or unknown reference")
        if any(label != label.strip() for label in rules["labels"]):
            raise ContractError("case.rules.labels", "labels must not have edge whitespace")
    elif category == "structured":
        _json_tree(rules["json_schema"], schema=True)
        _json_tree(rules["expected"])
        try:
            Draft202012Validator.check_schema(rules["json_schema"])
        except SchemaError as exc:
            raise ContractError("case.rules.json_schema", "invalid JSON Schema") from exc
        if not Draft202012Validator(rules["json_schema"]).is_valid(rules["expected"]):
            raise ContractError("case.rules.expected", "reference violates schema")
        if len(rules["fields"]) != len(set(rules["fields"])):
            raise ContractError("case.rules.fields", "duplicate scored path")
        for path in rules["fields"]:
            try:
                pointer(rules["expected"], path)
            except (KeyError, IndexError) as exc:
                raise ContractError("case.rules.fields", "reference path absent") from exc
            if any(other != path and other.startswith(path + "/") for other in rules["fields"]):
                raise ContractError("case.rules.fields", "overlapping scored paths")


def validate_bundle(bundle):
    _validate(bundle, BUNDLE, "bundle")
    return _validate_bundle_semantics(bundle)


def _validate_bundle_semantics(bundle):
    by_id = {}
    label_sets = set()
    for case in bundle["cases"]:
        # BUNDLE already validates every CASE; no new data enters this traversal.
        _validate_case_semantics(case)
        if case["case_id"] in by_id:
            raise ContractError("bundle.cases", "duplicate case_id")
        by_id[case["case_id"]] = case
        if (case["category"] == "performance") != (bundle["task_protocol"] == "performance"):
            raise ContractError("bundle.task_protocol", "mixed quality and performance tasks")
        if case["category"] == "classification":
            label_sets.add(tuple(case["rules"]["labels"]))
    if len(label_sets) > 1:
        raise ContractError("bundle.cases", "classification label set must be frozen per bundle")
    from inferyard.config.bundle_review import validate_provenance

    return validate_provenance(bundle)


def require_review(bundle):
    """Require an approval of current content or a verified equivalent legacy source."""
    from inferyard.config.bundle_review import approved, validate_provenance
    from inferyard.config.review_cache import review_cache

    cache = review_cache()
    digest = None
    if type(bundle) is Document and bundle.kind == "bundle":
        # The immutable snapshot already passed structure, semantics and proof checks.
        # Include approval records/proofs, unlike the human-review content_hash().
        snapshot = bundle._json.encode()
        if cache is not None:
            digest = hashlib.sha256(snapshot).hexdigest()
            if digest in cache:
                return
        bundle = bundle.to_dict()
        inherited = validate_provenance(bundle)
    else:
        # Public mutable inputs always receive complete validation.
        inherited = validate_bundle(bundle)
        if cache is not None:
            digest = hashlib.sha256(
                json.dumps(
                    bundle,
                    ensure_ascii=False,
                    sort_keys=True,
                    separators=(",", ":"),
                    allow_nan=False,
                ).encode()
            ).hexdigest()
            if digest in cache:
                return
    if approved(bundle) or any(inherited):
        if digest is not None:
            cache.add(digest)
        return
    raise ContractError("bundle.review_records", "human_corpus_review_required")
