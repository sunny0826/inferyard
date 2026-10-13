import pytest

from inferyard.analysis.input_lengths import (
    TOKEN_SOURCE,
    bind_token_counts,
    input_length_summary,
)
from inferyard.evidence.storage import EvidenceError
from tests.unit.test_observations import EVIDENCE, RUN, examples


def workload(kind="fixed"):
    return {
        "workload_id": "w",
        "protocol": {"kind": kind},
        "input_target_tokens": 1024,
        "output_budget_tokens": 512,
    }


def test_template_counts_bind_frozen_order_and_cannot_use_target_as_actual():
    budget = {
        "input_tokens": 31,
        "output_budget": 512,
        "template_prompt_sha256": "a" * 64,
        "source": TOKEN_SOURCE,
        "verification": "verified",
    }

    def envelope(formal):
        return {
            "definition": "token-budgets.v2",
            "entries": [
                {"phase": "probe", "case_id": None, "budget": {}},
                {"phase": "warmup", "case_id": None, "budget": {}},
                *[{"phase": "formal", "case_id": cid, "budget": row} for cid, row in formal],
            ],
        }

    assert (
        bind_token_counts(["c"], envelope([("c", budget)]), 512)["c"]["actual_input_tokens"] == 31
    )
    assert bind_token_counts(["c"], envelope([("c", {**budget, "output_budget": 128})]), 512) == {}
    assert bind_token_counts(["c"], envelope([("c", {})]), 512) == {}
    assert bind_token_counts(["c"], None, 512) == {}
    for selected, formal in (
        (["c"], []),
        (["c"], [("other", budget)]),
        (["c", "d"], [("d", budget), ("c", budget)]),
    ):
        with pytest.raises(EvidenceError, match="inventory"):
            bind_token_counts(selected, envelope(formal), 512)


def test_real_length_cohort_keeps_failure_denominator_and_latency_convention():
    _, rows = examples()
    counts = {
        r["case_id"]: {"actual_input_tokens": 31, "template_prompt_sha256": "a" * 64} for r in rows
    }
    summary, observations = input_length_summary(
        RUN, workload(), rows, counts, EVIDENCE, complete=True
    )
    cohort = summary["bins"][0]
    assert cohort["actual_input_tokens"] == 31 and cohort["input_target_tokens"] == 1024
    assert cohort["completion_rate"] == 3 / 4
    assert cohort["quality_rate"] == 2 / 4
    assert cohort["planned"] == 4 and cohort["failed"] == 1
    assert cohort["completed_latency_p50_ms"] == 2000
    assert not any(m["comparison_eligible"] for m in observations)


def test_missing_counts_never_produce_measured_length_cohort():
    _, rows = examples()
    summary, observations = input_length_summary(RUN, workload(), rows, {}, EVIDENCE, complete=True)
    assert summary["bins"][0]["actual_input_tokens"] is None
    assert all(m["value"] is None for m in observations)


def test_incomplete_or_repeated_scores_never_become_whole_corpus_quality():
    _, rows = examples()
    for complete, kind in ((False, "fixed"), (True, "duration")):
        summary, _ = input_length_summary(
            RUN, workload(kind), rows, {}, EVIDENCE, complete=complete
        )
        assert summary["bins"][0]["quality_rate"] is None
    rows[0]["score"] = None
    summary, _ = input_length_summary(RUN, workload(), rows, {}, EVIDENCE, complete=True)
    assert summary["bins"][0]["quality_rate"] is None
    assert summary["bins"][0]["unscorable_completed"] == 1


def test_frozen_tolerance_has_inclusive_bounds_and_no_unknown_substitution():
    from inferyard.analysis.input_lengths import check_input_target

    work = {**workload(), "input_tolerance_tokens": 4}
    for actual, expected in (
        (1020, "matched"),
        (1028, "matched"),
        (1019, "mismatch"),
        (1029, "mismatch"),
    ):
        result = check_input_target(work, ["c"], {"c": {"actual_input_tokens": actual}})
        assert result["status"] == expected
    assert check_input_target(work, ["c"], {})["status"] == "unverified"


def test_input_tolerance_requires_target_and_nonzero_lower_bound():
    from inferyard.contracts.validation import ContractError, validate_document
    from tests.unit.test_phase2_contracts import experiment

    for target, tolerance in ((None, 1), (10, 10), (10, 11)):
        data = experiment()
        data["workloads"][0].update(input_target_tokens=target, input_tolerance_tokens=tolerance)
        with pytest.raises(ContractError):
            validate_document("experiment", data)
