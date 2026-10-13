from copy import deepcopy

import pytest

from inferyard.analysis.comparison import compare_trials
from inferyard.analysis.engine_timing import DEFINITION
from tests.unit.test_comparison import trial


def pair(warmups=3):
    sides = [trial(), trial()]
    for data, policy in zip(sides, ("disabled", "enabled"), strict=True):
        data["plan"]["experiment"]["comparison"] = {"mode": "config", "factor": "cache_policy"}
        data["plan"]["experiment"]["workloads"][0]["cache_protocol"] = "prism_prefix_reuse.v1"
        data["config"]["conditions"]["cache_policy"] = policy
        data["config"]["execution"]["warmup_count"] = warmups
        data["requests"] = [
            {"request_id": f"r{3 + warmups}", "case_id": "case", "execution_state": "completed"}
        ]
        rows = []
        for i, phase in enumerate(["probe"] * 2 + ["warmup"] * warmups + ["formal"], 1):
            rows.append(
                dict(
                    ordinal=i,
                    request_id=f"r{i}",
                    case_id="case" if phase == "formal" else None,
                    phase=phase,
                    execution_state="completed",
                    requested_reuse=policy == "enabled" and i != 1,
                    cached_prompt_tokens=3 if policy == "enabled" and i != 1 else 0,
                    processed_prompt_tokens=1,
                    missing_reason=None,
                    source=DEFINITION,
                )
            )
        data["summary"]["cache_observations"] = {
            "definition": "observed_prefix_reuse_sequence.v1",
            "rows": rows,
        }
    return sides


@pytest.mark.parametrize("warmups", [0, 1, 2, 3])
def test_cache_qualification_uses_actual_frozen_warmup_history(warmups):
    left, right = pair(warmups)
    assert compare_trials(left, right)["eligibility"]["quality"]
    right["config"]["execution"]["warmup_count"] = 3 if warmups == 0 else 0
    result = compare_trials(left, right)
    assert not result["eligibility"]["quality"]


@pytest.mark.parametrize("warmups", [-1, 4, True, "0"])
def test_cache_qualification_still_rejects_invalid_warmup_history(warmups):
    from inferyard.analysis.cache_comparison import qualification

    _, right = pair()
    right["config"]["execution"]["warmup_count"] = warmups
    assert qualification(right, right["plan"]["experiment"]["workloads"][0]) == [
        "cache_warmup_history_unknown"
    ]


def test_cache_policy_can_differ_but_does_not_grant_performance_or_require_hits():
    left, right = pair()
    result = compare_trials(left, right)
    assert result["eligibility"]["quality"]
    assert result["eligibility"]["completion"]
    assert not result["eligibility"]["performance"]
    for row in right["summary"]["cache_observations"]["rows"]:
        row["cached_prompt_tokens"] = 0
    assert compare_trials(left, right)["eligibility"]["quality"]
    right["config"]["conditions"]["threads"] = 8
    assert not compare_trials(left, right)["eligibility"]["quality"]


@pytest.mark.parametrize(
    "change",
    [
        "protocol",
        "missing",
        "history",
        "count",
        "reset",
        "policy",
        "state",
        "formal",
        "duplicate",
        "source",
        "warmup",
        "unchanged",
    ],
)
def test_cache_history_gates_cannot_be_bypassed_by_summary_flags(change):
    left, right = pair()
    observation = right["summary"]["cache_observations"]
    observation.update(
        policy_consistent=True, initial_no_reuse_observed=True, comparison_eligible=True
    )
    rows = observation["rows"]
    if change == "protocol":
        right["plan"]["experiment"]["workloads"][0].pop("cache_protocol")
    elif change == "missing":
        right["summary"].pop("cache_observations")
    elif change == "history":
        rows.pop(3)
    elif change == "count":
        rows[4]["cached_prompt_tokens"] = None
    elif change == "reset":
        rows[0]["cached_prompt_tokens"] = 1
    elif change == "policy":
        rows[1]["requested_reuse"] = False
    elif change == "state":
        rows[3]["execution_state"] = "failed"
    elif change == "formal":
        rows[-1]["case_id"] = "other"
    elif change == "duplicate":
        rows[3]["request_id"] = rows[2]["request_id"]
    elif change == "source":
        rows[2]["source"] = "unknown"
    elif change == "warmup":
        right["config"]["execution"].pop("warmup_count")
    else:
        right = deepcopy(left)
    result = compare_trials(left, right)
    assert not result["eligibility"]["quality"]
    assert result["completion_rate_difference"] is None
