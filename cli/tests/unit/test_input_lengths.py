import pytest

from inferyard.analysis.input_lengths import (
    TOKEN_SOURCE,
    bind_token_counts,
    input_length_summary,
)
from inferyard.errors import ContractError, EvidenceError
from tests.unit.test_observations import EVIDENCE, RUN, examples


def workload(kind="fixed"):
    return {
        "workload_id": "w",
        "protocol": {"kind": kind},
        "input_target_tokens": 1024,
        "output_budget_tokens": 512,
    }


def test_missing_counts_never_produce_measured_length_cohort():
    _, rows = examples()
    summary, observations = input_length_summary(RUN, workload(), rows, {}, EVIDENCE, complete=True)
    assert summary["bins"][0]["actual_input_tokens"] is None
    assert all(m["value"] is None for m in observations)
