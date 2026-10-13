"""Offline-only scoring revision: separate numeric syntax and value diagnostics."""

import hashlib
from pathlib import Path

from inferyard.analysis import scoring as original
from inferyard.contracts.validation import validate_document
from inferyard.evidence.storage import EvidenceError

VERSION = "phase2.v2"


def scorer_hash(*, original_hash=None):
    digest = hashlib.sha256((original_hash or original.scorer_hash()).encode())
    digest.update(Path(__file__).read_bytes())
    return digest.hexdigest()


def identify(result, identity):
    result.update(
        scorer_id=f"{result['category']}.{VERSION}",
        scorer_version=VERSION,
        scorer_sha256=identity,
    )
    return result


def revise(result, identity):
    if result["category"] == "math":
        numeric = result["rule_results"][0]
        result["rule_results"] = [
            original._rule("numeric_unit_format", True, result["format_ok"], result["format_ok"]),
            {**numeric, "rule": "numeric_tolerance_match"},
        ]
    return identify(result, identity)


def score_case(case, answer, policy):
    context = original.ScoringContext()
    context.prepare([case])
    result = context.score(case["case_id"], answer, policy, VERSION)
    validate_document("score", result)
    return result


def unscorable(category, reason):
    result = original.ScoringContext().missing(category, reason, VERSION)
    validate_document("score", result)
    return result


def resolve(version):
    if version == original.SCORER_VERSION:
        return original.score_case, original.scorer_hash, original.unscorable
    if version == VERSION:
        return score_case, scorer_hash, unscorable
    raise EvidenceError("unsupported_rescore_version")
