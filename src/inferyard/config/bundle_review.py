"""Explicit review inheritance; legacy bytes are opaque proof, never active input.

The original human records and hashes remain unchanged. Historical v1/v2 proofs
retain exact equivalence. Per-case approval requires explicit human records; an
old whole-bundle signature never becomes a per-case approval.
"""

import hashlib
import json
from copy import deepcopy

from inferyard import SCHEMA_VERSION
from inferyard.contracts.schemas_tasks import BUNDLE
from inferyard.contracts.validation import ContractError, _validate, strict_json_loads

PROOF_PATH = "bundle.review_provenance"
BASE_FIELDS = {
    "schema_version",
    "bundle_id",
    "version",
    "language",
    "license_note",
    "review_records",
    "answer_policy",
    "cases",
}


def _canonical(value):
    return json.dumps(
        value, ensure_ascii=False, allow_nan=False, sort_keys=True, separators=(",", ":")
    )


def approved(bundle):
    from inferyard.config.bundle import content_hash

    digest = content_hash(bundle)
    return any(
        r["content_sha256"] == digest
        and r["conclusion"] == "approved"
        and r["reviewer"].strip()
        and r["reviewed_at"].strip()
        for r in bundle["review_records"]
    )


def _semantic_content(bundle):
    return {
        key: value
        for key, value in bundle.items()
        if key not in ("schema_version", "review_records", "review_provenance")
    }


def _legacy_source(source, *, inherited=False):
    """Validate only the two frozen historical corpus forms, without a registry."""
    if type(source) is not dict or type(source.get("schema_version")) is not int:
        raise ContractError(PROOF_PATH, "legacy corpus must declare its original revision")
    revision = source["schema_version"]
    expected = BASE_FIELDS | ({"inherited_bundles", "task_protocol"} if revision == 2 else set())
    if revision not in (1, 2) or (inherited and revision != 1) or set(source) != expected:
        raise ContractError(PROOF_PATH, "unsupported legacy corpus shape")
    current = deepcopy(source)
    current["schema_version"] = SCHEMA_VERSION
    current.setdefault("task_protocol", "quality")
    ancestors = current.pop("inherited_bundles", [])
    _validate(current, BUNDLE, PROOF_PATH)
    if revision == 1 and any(
        case["category"] not in ("instruction", "extraction") for case in current["cases"]
    ):
        raise ContractError(PROOF_PATH, "legacy revision has unsupported task categories")
    if type(ancestors) is not list:
        raise ContractError(PROOF_PATH, "legacy inheritance must be an array")
    from inferyard.config.bundle import validate_bundle

    validate_bundle(current)
    by_id = {case["case_id"]: case for case in current["cases"]}
    seen = set()
    all_approved = bool(approved(source))
    for ancestor in ancestors:
        normalized, ancestor_approved = _legacy_source(ancestor, inherited=True)
        if normalized["answer_policy"] != current["answer_policy"]:
            raise ContractError(PROOF_PATH, "inherited answer policy differs")
        for case in normalized["cases"]:
            if case["case_id"] in seen or by_id.get(case["case_id"]) != case:
                raise ContractError(PROOF_PATH, "inherited case differs or is duplicated")
            seen.add(case["case_id"])
        all_approved &= bool(ancestor_approved)
    return current, all_approved


def validate_provenance(bundle):
    """Return approval flags after verifying all proof hashes and equivalence."""
    from inferyard.config.bundle import content_hash

    result, seen = [], set()
    covered = approved_cases(bundle)
    for proof in bundle.get("review_provenance", []):
        raw = proof["source_json"]
        digest = hashlib.sha256(raw.encode("utf-8")).hexdigest()
        if digest != proof["source_sha256"] or digest in seen:
            raise ContractError(PROOF_PATH, "legacy proof hash differs or is duplicated")
        seen.add(digest)
        source = strict_json_loads(raw)
        current, inherited_approval = _legacy_source(source)
        if content_hash(source) != proof["source_content_sha256"]:
            raise ContractError(PROOF_PATH, "legacy content hash differs")
        if _canonical(_semantic_content(current)) != _canonical(_semantic_content(bundle)):
            raise ContractError(PROOF_PATH, "current content differs from approved legacy corpus")
        retained = {_canonical(record) for record in bundle["review_records"]}
        if any(_canonical(record) not in retained for record in source["review_records"]):
            raise ContractError(PROOF_PATH, "original review record was removed or changed")
        result.append(inherited_approval)
    result.append(covered == {case["case_id"] for case in bundle["cases"]})
    return result


def upgrade_legacy_bundle(source):
    """Copy dict input, or preserve exact JSON text; never synthesize approval.

    Unreviewed corpora may be migrated for diagnostic use, but require_review
    continues to reject them. Callers must keep the original artifact as well.
    """
    from inferyard.config.bundle import content_hash, validate_bundle

    raw = source if type(source) is str else _canonical(source)
    source = strict_json_loads(raw)
    current, _ = _legacy_source(source)
    current["review_provenance"] = [
        {
            "source_json": raw,
            "source_sha256": hashlib.sha256(raw.encode("utf-8")).hexdigest(),
            "source_content_sha256": content_hash(source),
        }
    ]
    validate_bundle(current)
    return current


def case_content_hash(bundle, case):
    """Review identity excludes display/license metadata, never execution content."""
    value = {
        "definition": "case-review.v1",
        **case,
        "task_protocol": bundle["task_protocol"],
        "answer_policy": bundle["answer_policy"],
    }
    return hashlib.sha256(_canonical(value).encode()).hexdigest()


def approved_cases(bundle):
    records = {
        (r["case_id"], r["content_sha256"])
        for r in bundle.get("case_review_records", [])
        if r["conclusion"] == "approved" and r["reviewer"].strip() and r["reviewed_at"].strip()
    }
    return {
        case["case_id"]
        for case in bundle["cases"]
        if (case["case_id"], case_content_hash(bundle, case)) in records
    }
