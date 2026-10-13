"""Objective scoring without execution, answer repair, network or model judges."""

import hashlib
import json
import re
import unicodedata
from copy import deepcopy
from decimal import MAX_EMAX, MIN_EMIN, Decimal, DecimalException, localcontext
from importlib.metadata import version
from importlib.resources import files

from jsonschema import Draft202012Validator

from inferyard import SCHEMA_VERSION
from inferyard.analysis.scoring_rules import score_rules
from inferyard.config.bundle import pointer, validate_case
from inferyard.contracts.validation import ContractError, strict_json_loads, validate_document

SCORER_VERSION = "phase2.v1"
CATEGORIES = (
    "instruction",
    "extraction",
    "qa",
    "math",
    "classification",
    "structured",
    "performance",
    "svg",
)


def scorer_hash():
    digest = hashlib.sha256()
    root = files("inferyard")
    for name in (
        "analysis/scoring.py",
        "analysis/scoring_rules.py",
        "config/bundle.py",
        "config/bundle_review.py",
        "contracts/validation.py",
        "contracts/contracts_experiment.py",
        "contracts/schemas_tasks.py",
        "contracts/schemas_common.py",
        "__init__.py",
    ):
        digest.update(name.encode() + b"\0")
        digest.update(root.joinpath(name).read_bytes())
    digest.update(version("jsonschema").encode())
    digest.update(unicodedata.unidata_version.encode())
    return digest.hexdigest()


def typed_equal(left, right):
    if type(left) in (int, float) and type(right) in (int, float):
        return left == right
    if type(left) is not type(right):
        return False
    if type(left) is dict:
        return left.keys() == right.keys() and all(typed_equal(left[k], right[k]) for k in left)
    if type(left) is list:
        return len(left) == len(right) and all(
            typed_equal(a, b) for a, b in zip(left, right, strict=True)
        )
    return left == right


def _parse(answer):
    try:
        return True, strict_json_loads(answer)
    except ContractError:
        return False, None


def _display(value):
    return json.dumps(value, ensure_ascii=False, allow_nan=False, sort_keys=True)


def _rule(name, expected, observed, passed):
    return {
        "rule": name,
        "expected": _display(expected),
        "observed": _display(observed),
        "passed": bool(passed),
        "reason": "matched" if passed else "not_matched",
    }


def _empty(category, identity):
    return {
        "schema_version": SCHEMA_VERSION,
        "category": category,
        "scorer_id": f"{category}.{SCORER_VERSION}",
        "scorer_version": SCORER_VERSION,
        "scorer_sha256": identity,
        "quality_state": "fail",
        "format_ok": None,
        "content_ok": False,
        "rule_results": [],
        "explanation": "required_rule_failed",
        "reason": None,
        "json_parse_ok": None,
        "schema_ok": None,
        "field_results": [],
        "constraint_results": [],
        "expected_label": None,
        "predicted_label": None,
    }


def _qa(case, answer, result):
    rules = case["rules"]

    def normalize(text):
        if rules["normalization"] == "nfkc_strip":
            text = unicodedata.normalize("NFKC", text)
        return text.strip()

    observed = normalize(answer)
    passed = observed in [normalize(value) for value in rules["answers"]]
    result["rule_results"] = [_rule("exact_match", rules["answers"], observed, passed)]


def _math(case, answer, result):
    rules = case["rules"]
    match = re.fullmatch(
        r"([+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?)\s*"
        + re.escape(rules["unit"]),
        answer.strip(),
    )
    passed = False
    observed = None
    if match:
        try:
            actual, expected = Decimal(match[1]), Decimal(str(rules["expected"]))
            absolute = Decimal(str(rules["absolute_tolerance"]))
            relative = Decimal(str(rules["relative_tolerance"]))
            # Size arithmetic from frozen constants, never an answer's hostile exponent.
            # Comparing exact bounds avoids rounding tiny nonzero answers down to zero.
            with localcontext() as context:
                context.prec = 32 + 2 * sum(
                    len(value.as_tuple().digits) + abs(value.as_tuple().exponent)
                    for value in (expected, absolute, relative)
                )
                context.Emax, context.Emin = MAX_EMAX, MIN_EMIN
                tolerance = max(absolute, relative * abs(expected))
                lower, upper = expected - tolerance, expected + tolerance
                passed = actual.is_finite() and lower <= actual <= upper
            observed = str(actual)
        except DecimalException, OverflowError:
            pass
    result["format_ok"] = bool(match)
    result["rule_results"] = [_rule("numeric_with_unit", rules["expected"], observed, passed)]


def _classification(case, answer, result):
    rules = case["rules"]
    observed = answer.strip()
    predicted = observed if observed in rules["labels"] else None
    result.update(
        expected_label=rules["expected"], predicted_label=predicted, format_ok=predicted is not None
    )
    result["rule_results"] = [
        _rule("label", rules["expected"], predicted, predicted == rules["expected"])
    ]


def _structured(case, answer, result, validator):
    rules = case["rules"]
    parsed, value = _parse(answer)
    schema_ok = parsed and validator.is_valid(value)
    result.update(json_parse_ok=parsed, schema_ok=bool(schema_ok), format_ok=bool(schema_ok))
    result["rule_results"] = [
        _rule("json_parse", True, parsed, parsed),
        _rule("json_schema", True, bool(schema_ok), schema_ok),
    ]
    for path in rules["fields"]:
        observed, passed = None, False
        if parsed:
            try:
                observed = pointer(value, path)
                passed = typed_equal(observed, pointer(rules["expected"], path))
            except KeyError, IndexError:
                pass
        result["field_results"].append({"path": path, "passed": passed})
        result["rule_results"].append(
            _rule("field:" + path, pointer(rules["expected"], path), observed, passed)
        )


SCORERS = {"qa": _qa, "math": _math, "classification": _classification, "structured": _structured}


def _score_prepared(case, answer, policy, identity, validator):
    category = case["category"]
    result = _empty(category, identity)
    if category in ("instruction", "extraction"):
        score_rules(case, answer, policy, result)
    elif category in ("performance", "svg"):
        result.update(
            quality_state="not_applicable",
            content_ok=None,
            explanation=f"{category}_task_without_quality_score",
        )
    else:
        if category == "structured":
            _structured(case, answer, result, validator)
        else:
            SCORERS[category](case, answer, result)
        passed = bool(result["rule_results"]) and all(r["passed"] for r in result["rule_results"])
        result.update(
            quality_state="pass" if passed else "fail",
            content_ok=passed,
            explanation="all_required_rules_matched" if passed else "required_rule_failed",
        )
    return result


def _unscorable(category, reason, identity):
    result = _empty(category, identity)
    result.update(
        quality_state="not_applicable" if category in ("performance", "svg") else "unscorable",
        content_ok=None,
        explanation=reason,
        reason=reason,
    )
    return result


class ScoringContext:
    """One command's identities and detached rules; never a path/global cache."""

    def __init__(self):
        self._identities = {}
        self._cases = {}

    def identity(self, version=SCORER_VERSION):
        if version not in self._identities:
            if version == SCORER_VERSION:
                self._identities[version] = scorer_hash()
            else:
                from inferyard.analysis import scoring_revision
                from inferyard.evidence.storage import EvidenceError

                if version != scoring_revision.VERSION:
                    raise EvidenceError("unsupported_rescore_version")
                self._identities[version] = scoring_revision.scorer_hash(
                    original_hash=self.identity()
                )
        return self._identities[version]

    def prepare(self, cases):
        for case in cases:
            cid = case["case_id"]
            previous = self._cases.get(cid)
            if previous is not None and previous[0] == case:
                continue
            validate_case(case)
            frozen = deepcopy(case)
            validator = (
                Draft202012Validator(frozen["rules"]["json_schema"])
                if frozen["category"] == "structured"
                else None
            )
            self._cases[cid] = frozen, validator

    def score(self, case_id, answer, policy, version=SCORER_VERSION):
        case, validator = self._cases[case_id]
        result = _score_prepared(case, answer, policy, self.identity(), validator)
        if version != SCORER_VERSION:
            from inferyard.analysis.scoring_revision import revise

            result = revise(result, self.identity(version))
        return result

    def missing(self, category, reason, version=SCORER_VERSION):
        result = _unscorable(category, reason, self.identity())
        if version != SCORER_VERSION:
            from inferyard.analysis.scoring_revision import identify

            return identify(result, self.identity(version))
        return result


def score_case(case, answer, policy):
    context = ScoringContext()
    context.prepare([case])
    result = context.score(case["case_id"], answer, policy)
    validate_document("score", result)
    return result


def unscorable(category, reason):
    result = ScoringContext().missing(category, reason)
    validate_document("score", result)
    return result
