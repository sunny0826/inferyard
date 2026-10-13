"""A-batch reuse must preserve old reductions and validate each new command's input."""

import gzip
import json
from collections import Counter
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.analysis import scoring, scoring_revision
from inferyard.application.types import CommandRequest
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import EvidenceError, json_bytes
from inferyard.registry import components
from inferyard.reporting import comparison_report, rescore
from inferyard.reporting.report import build_index, verify_report
from tests.helpers import fixture_run
from tests.unit.test_scoring_contract import BUNDLE, POLICY


def spy_identities(monkeypatch):
    counts = Counter()
    for module in (scoring, scoring_revision):
        original = module.scorer_hash

        def counted(*args, _fn=original, _name=module.__name__, **kwargs):
            counts[_name] += 1
            return _fn(*args, **kwargs)

        monkeypatch.setattr(module, "scorer_hash", counted)
    return counts


def test_context_prepares_rules_once_and_detaches_input(monkeypatch):
    counts = spy_identities(monkeypatch)
    prepared, validators = [], []
    original, validator = scoring.validate_case, scoring.Draft202012Validator

    def prepare(case):
        prepared.append(case["case_id"])
        original(case)

    def construct(schema):
        validators.append(schema)
        return validator(schema)

    monkeypatch.setattr(scoring, "validate_case", prepare)
    monkeypatch.setattr(scoring, "Draft202012Validator", construct)
    cases = deepcopy(BUNDLE["cases"])
    context = scoring.ScoringContext()
    context.prepare(cases)
    for _ in range(3):
        context.prepare(cases)
        for case in cases:
            for version in ("phase2.v1", "phase2.v2"):
                result = context.score(case["case_id"], case["reference_answer"], POLICY, version)
                assert result["quality_state"] in ("pass", "not_applicable")
    assert prepared == [c["case_id"] for c in cases]
    assert len(validators) == sum(c["category"] == "structured" for c in cases)
    assert list(counts.values()) == [1, 1]
    first = cases[0]
    expected = context.score(first["case_id"], first["reference_answer"], POLICY)
    first["rules"].clear()
    assert context.score(first["case_id"], first["reference_answer"], POLICY) == expected
    with pytest.raises(ContractError):
        scoring.score_case(first, first["reference_answer"], POLICY)


def test_catalogue_reads_scoring_sources_only_once(monkeypatch):
    counts = spy_identities(monkeypatch)
    components()
    assert counts[scoring.__name__] == 1


def test_recursive_rescore_uses_one_identity_per_version_and_one_reduction(tmp_path, monkeypatch):
    root = fixture_run(tmp_path / "source")
    first, second = tmp_path / "first", tmp_path / "second"
    rescore.write_rescore(root, first, scorer_id="phase2.v1", reason="first")
    counts = spy_identities(monkeypatch)
    reads = []
    original = comparison_report.read_trial

    def counted(*args, **kwargs):
        reads.append(args[0])
        return original(*args, **kwargs)

    monkeypatch.setattr(comparison_report, "read_trial", counted)
    rescore.write_rescore(
        root, second, scorer_id="phase2.v2", reason="second", parent_path=first / "analysis.json"
    )
    assert counts == {scoring.__name__: 1, scoring_revision.__name__: 1}
    assert reads == [root]
    counts.clear()
    reads.clear()
    assert rescore.verify_rescore(second)["verified"]
    assert counts == {scoring.__name__: 1, scoring_revision.__name__: 1}
    assert reads == [root]
    monkeypatch.setattr(scoring, "scorer_hash", lambda: "f" * 64)
    with pytest.raises(EvidenceError, match="rescore_scorer_identity_changed"):
        rescore.verify_rescore(second)
    # Origin corruption is still checked before an unavailable scorer is reported.
    with (root / "events.jsonl").open("ab") as stream:
        stream.write(b"\n")
    with pytest.raises(EvidenceError, match="original_evidence_hash_mismatch"):
        rescore.verify_rescore(second)


def test_compare_reduces_each_source_once_and_compares_once(tmp_path, monkeypatch):
    roots = [fixture_run(tmp_path / side) for side in ("left", "right")]
    reads, compares = [], []
    original_read, original_compare = comparison_report.read_trial, comparison_report.compare_trials

    def read(*args, **kwargs):
        reads.append(args[0])
        return original_read(*args, **kwargs)

    def compare(*args, **kwargs):
        compares.append(1)
        return original_compare(*args, **kwargs)

    monkeypatch.setattr(comparison_report, "read_trial", read)
    monkeypatch.setattr(comparison_report, "compare_trials", compare)
    out = tmp_path / "comparison"
    code, _ = comparison_report.execute(
        CommandRequest("compare", left=roots[0], right=roots[1], out=out)
    )
    assert code == 0 and reads == roots and compares == [1]
    assert verify_report(out)["verified"]
    # A new command reads new bytes and rejects changed original evidence.
    (roots[0] / "memory.jsonl").write_text("changed")
    with pytest.raises(EvidenceError, match="original_evidence_hash_mismatch"):
        comparison_report.verify_comparison(out)


def test_core_logs_are_read_once_and_facets_do_not_reopen_events(tmp_path, monkeypatch):
    root = fixture_run(tmp_path / "source")
    counts = Counter()
    original = Path.open

    def opened(path, *args, **kwargs):
        if path.parent == root:
            counts[path.name] += 1
        return original(path, *args, **kwargs)

    monkeypatch.setattr(Path, "open", opened)
    build_index([root], tmp_path / "report")
    assert counts["events.jsonl"] == counts["memory.jsonl"] == 1
    assert counts["config.frozen.json"] == counts["bundle.json"] == 1


def test_pre_a_ledger_comparison_and_all_report_indexes_remain_equivalent(tmp_path):
    from inferyard.evidence.formats import UnsupportedFormat

    saved = json.loads(
        gzip.decompress(
            (Path(__file__).parents[1] / "fixtures/pre_a_reduction.json.gz").read_bytes()
        )
    )
    value = saved["comparison"]
    (tmp_path / "comparison.json").write_bytes(json_bytes(value))
    with pytest.raises(UnsupportedFormat):
        comparison_report.read_verified_comparison(tmp_path)


def test_scoring_source_bytes_read_once_for_each_required_version(monkeypatch):
    package = Path(scoring.__file__).parents[1]
    reads = Counter()
    original = Path.read_bytes

    def read(path):
        if path.is_relative_to(package):
            reads[path.relative_to(package).as_posix()] += 1
        return original(path)

    monkeypatch.setattr(Path, "read_bytes", read)
    context = scoring.ScoringContext()
    cases = BUNDLE["cases"][:3]
    context.prepare(cases)
    for _ in range(3):
        for case in cases:
            for version in ("phase2.v1", "phase2.v2"):
                context.score(case["case_id"], case["reference_answer"], POLICY, version)
    assert len(reads) == 10  # Nine v1 files plus the v2 wrapper, for the entire command.
    assert set(reads.values()) == {1}


@pytest.mark.parametrize("damage", ["shape", "identity", "category"])
def test_custom_scorer_result_still_checked_and_does_not_leak(damage):
    case = BUNDLE["cases"][0]
    source = [
        {
            "case_id": case["case_id"],
            "category": case["category"],
            "request_id": "x",
            "execution_state": "completed",
            "content": case["reference_answer"],
            "score": None,
        }
    ]

    def custom(*_):
        result = scoring.score_case(case, case["reference_answer"], POLICY)
        if damage == "shape":
            result["private-response"] = "secret"
        elif damage == "identity":
            result["scorer_sha256"] = "0" * 64
        else:
            result["category"] = "math"
        return result

    if damage in ("identity", "category"):
        with pytest.raises(EvidenceError, match="rescore_scorer_identity_mismatch"):
            rescore.rescore_rows([case], source, POLICY, scorer=custom)
        return
    rows, _ = rescore.rescore_rows([case], source, POLICY, scorer=custom)
    assert rows[0]["score"]["reason"] == "scorer_exception"
    assert "secret" not in str(rows)


def test_explicit_custom_identity_is_not_bypassed_by_builtin_scorer():
    case = BUNDLE["cases"][0]
    row = {
        "case_id": case["case_id"],
        "category": case["category"],
        "request_id": "x",
        "execution_state": "completed",
        "content": case["reference_answer"],
        "score": None,
    }
    calls = []

    def identity():
        calls.append(1)
        return "0" * 64

    with pytest.raises(EvidenceError, match="rescore_scorer_identity_mismatch"):
        rescore.rescore_rows([case], [row, row], POLICY, identity=identity)
    assert calls == [1]


def test_pre_c_ledger_comparison_v2_and_report_v5_remain_equivalent(tmp_path):
    from inferyard.evidence.formats import UnsupportedFormat

    saved = json.loads(
        gzip.decompress(
            (Path(__file__).parents[1] / "fixtures/pre_c_reduction.json.gz").read_bytes()
        )
    )
    value = saved["comparison"]
    (tmp_path / "comparison.json").write_bytes(json_bytes(value))
    with pytest.raises(UnsupportedFormat):
        comparison_report.read_verified_comparison(tmp_path)
