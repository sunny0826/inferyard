"""Internal engine-comparison.v2 run set, handoff chain and history stop (ADR 057c)."""

from copy import deepcopy

import pytest

from inferyard.analysis.comparison_history import (
    HISTORY_CANCELLED,
    HISTORY_FAILED,
    HISTORY_IDENTITY_ERROR,
    HISTORY_INCOMPLETE,
)
from inferyard.analysis.engine_comparison_v2 import (
    HANDOFF_BROKEN,
    HANDOFF_MISSING,
    HANDOFF_SLOT_MISMATCH,
    PLANNED_RUN_MISSING,
    RUN_PLAN_MISMATCH,
    UNPLANNED_TRIAL,
    bundle_comparisons,
    comparison_history_stop,
    evaluate_comparison,
)
from inferyard.config.comparison_plan import comparison_slots, freeze
from tests.unit.test_comparison_plan import comparison_inputs
from tests.unit.test_engine_blocks import block, request


def plan_with_slots(tmp_path):
    left, right, specs = comparison_inputs(tmp_path)
    plan = freeze(left, right, specs, experiment_id="engine-compare-v2-fixture")
    return plan, comparison_slots(plan)


def run_id(slot):
    return f"run-{slot.block}-{slot.side}-{slot.bundle_key}"


def synthetic_runs(plan, *, same_plan=True):
    slots = comparison_slots(plan)
    entries, previous = [], None
    for slot in slots:
        identifier = run_id(slot)
        entries.append(
            {
                "path": tmp_path_path(slot),
                "run": {
                    "run_id": identifier,
                    "trial_id": slot.trial_id,
                    "plan_sha256": plan["plan_sha256"] if same_plan else "f" * 64,
                    "experiment_id": plan["experiment"]["experiment_id"],
                    "definition_versions": plan["experiment"]["definition_versions"],
                    "diagnostic": False,
                    "relation": "initial",
                    "kind": "run",
                },
                "summary": {
                    "stop_reason": "plan_finished",
                    "scope_complete": True,
                    "evidence_complete": True,
                },
                "requests": [
                    {"execution_state": "completed", "quality_state": "pass"},
                    {"execution_state": "completed", "quality_state": "fail"},
                ],
                "service_binding": {"previous_run_id": previous},
            }
        )
        previous = identifier
    return entries


def tmp_path_path(slot):
    return f"/runs/{slot.trial_id}"


def reasons(document):
    return [row["reason"] for row in document["diagnostics"]]


def test_complete_frozen_chain_is_eligible(tmp_path):
    plan, slots = plan_with_slots(tmp_path)
    document = evaluate_comparison(plan, synthetic_runs(plan))
    assert document["eligible"] is True, document["diagnostics"]
    assert document["run_set"]["expected_trials"] == [slot.trial_id for slot in slots]
    assert len(document["run_set"]["observed_runs"]) == 12
    assert {row["status"] for row in document["handoff"]["slots"]} == {"ok"}
    assert document["history_stop"] is None
    assert len(document["bundles"]) == 2
    for group in document["bundles"].values():
        assert len(group["left"]) == 3 and len(group["right"]) == 3
        assert len(group["blocks"]) == 3


def test_missing_run_is_reported_once(tmp_path):
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    del runs[5]
    document = evaluate_comparison(plan, runs)
    assert reasons(document) == [PLANNED_RUN_MISSING]
    assert document["run_set"]["missing"][0]["trial_id"] == plan["trials"][5]["trial_id"]


def test_unplanned_trial_uses_the_canonical_reason(tmp_path):
    plan, slots = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs.append(
        {
            **deepcopy(runs[0]),
            "run": {**runs[0]["run"], "run_id": "run-extra", "trial_id": "trial-extra"},
        }
    )
    document = evaluate_comparison(plan, runs)
    assert UNPLANNED_TRIAL in reasons(document)
    assert "unplanned_run_present" not in {
        row.get("reason") for row in document["run_set"]["unplanned"]
    }
    assert "execution_order_differs" not in reasons(document)


def test_run_with_another_plan_hash_is_mismatched(tmp_path):
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[3]["run"]["plan_sha256"] = "f" * 64
    document = evaluate_comparison(plan, runs)
    assert reasons(document) == [RUN_PLAN_MISMATCH]


def test_duplicate_trial_attempt_blocks(tmp_path):
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    duplicate = deepcopy(runs[0])
    duplicate["run"]["run_id"] = "run-resumed"
    runs.append(duplicate)
    document = evaluate_comparison(plan, runs)
    assert "comparison_duplicate_trial_attempt" in reasons(document)
    assert document["run_set"]["duplicates"][0]["trial_id"] == plan["trials"][0]["trial_id"]


def test_first_slot_with_predecessor_is_rejected(tmp_path):
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[0]["service_binding"]["previous_run_id"] = "run-before"
    document = evaluate_comparison(plan, runs)
    assert reasons(document) == [HANDOFF_SLOT_MISMATCH]


def test_missing_sealed_binding_is_rejected(tmp_path):
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[1]["service_binding"] = None
    document = evaluate_comparison(plan, runs)
    assert reasons(document) == [HANDOFF_MISSING]


def test_empty_expected_predecessor_is_rejected(tmp_path):
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[2]["service_binding"]["previous_run_id"] = None
    document = evaluate_comparison(plan, runs)
    assert reasons(document) == [HANDOFF_MISSING]


@pytest.mark.parametrize("index", [2, 3])
def test_chain_must_point_to_the_previous_slot(tmp_path, index):
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[index]["service_binding"]["previous_run_id"] = runs[0]["run"]["run_id"]
    document = evaluate_comparison(plan, runs)
    assert reasons(document) == [HANDOFF_BROKEN]


def test_same_plan_across_batches_cannot_splice_slots(tmp_path):
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)[:2]
    runs[1]["service_binding"]["previous_run_id"] = None
    document = evaluate_comparison(plan, runs)
    assert HANDOFF_MISSING in reasons(document)
    assert document["eligible"] is False


def test_reader_order_and_event_time_never_override_plan_order(tmp_path):
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    reversed_runs = list(reversed(runs))
    document = evaluate_comparison(plan, reversed_runs)
    assert document["eligible"] is True, document["diagnostics"]
    slots = comparison_slots(plan)
    assert document["handoff"]["slots"][0]["trial_id"] == slots[0].trial_id
    assert document["handoff"]["slots"][-1]["trial_id"] == slots[-1].trial_id


@pytest.mark.parametrize(
    ("mutate", "expected"),
    [
        (
            lambda runs: runs[1]["summary"].update(stop_reason="user_cancelled"),
            HISTORY_CANCELLED,
        ),
        (
            lambda runs: runs[2]["requests"][0].update(execution_state="cancelled"),
            HISTORY_CANCELLED,
        ),
        (
            lambda runs: runs[3]["requests"][0].update(execution_state="failed"),
            HISTORY_FAILED,
        ),
        (
            lambda runs: runs[4]["summary"].update(stop_reason="hardware_stop"),
            HISTORY_FAILED,
        ),
        (
            lambda runs: runs[5]["summary"].update(scope_complete=False),
            HISTORY_INCOMPLETE,
        ),
    ],
)
def test_terminal_history_blocks_further_slots(tmp_path, mutate, expected):
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    mutate(runs)
    document = evaluate_comparison(plan, runs)
    assert expected in reasons(document)
    assert document["history_stop"]["reason"] == expected
    assert document["eligible"] is False


def test_subsequent_run_after_a_stop_is_reported(tmp_path):
    plan, slots = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[1]["summary"]["stop_reason"] = "user_cancelled"
    stop = comparison_history_stop(plan, runs)
    assert stop["reason"] == HISTORY_CANCELLED
    assert stop["trial_id"] == slots[1].trial_id
    assert runs[2]["run"]["run_id"] in stop["subsequent_run_ids"]


def test_normal_handoff_and_quality_outcomes_do_not_stop(tmp_path):
    plan, slots = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)[:2]
    assert comparison_history_stop(plan, runs) is None
    runs = synthetic_runs(plan)
    for entry in runs:
        for row in entry["requests"]:
            row["quality_state"] = "unscorable"
    assert comparison_history_stop(plan, runs) is None


def test_resume_attempt_and_later_run_after_gap_are_identity_errors(tmp_path):
    plan, slots = plan_with_slots(tmp_path)
    resumed = synthetic_runs(plan)
    child = deepcopy(resumed[0])
    child["run"]["run_id"] = "run-resume"
    resumed.append(child)
    stop = comparison_history_stop(plan, resumed)
    assert stop["reason"] == HISTORY_IDENTITY_ERROR
    assert stop["detail"] == "duplicate_trial_attempt"
    gap = synthetic_runs(plan)
    del gap[1]
    stop = comparison_history_stop(plan, gap)
    assert stop["reason"] == HISTORY_IDENTITY_ERROR
    assert stop["detail"] == "subsequent_run_present"
    assert stop["trial_id"] == slots[1].trial_id


def test_per_bundle_blocks_use_the_existing_six_block_verdict(tmp_path):
    plan, slots = plan_with_slots(tmp_path)
    data = {}
    for key, right_ms in (("core", 800), ("pelican", 1200)):
        for slot in (item for item in slots if item.bundle_key == key):
            order = slot.block - 1
            family = "llama-cpp" if slot.side == "a" else "lmstudio"
            row = block(family, order, e2e_ms=right_ms if slot.side == "b" else 1000)
            row["run"].update(run_id=run_id(slot), trial_id=slot.trial_id)
            row["plan"]["trials"] = [{"trial_id": slot.trial_id, "workload_id": "w"}]
            row["selection"].update(bundle_sha256=key, case_ids=["case"])
            row["requests"] = [
                request(index, e2e_ms=right_ms if slot.side == "b" else 1000)
                for index in range(2)
            ]
            data[run_id(slot)] = row
    document = evaluate_comparison(plan, synthetic_runs(plan))
    results = bundle_comparisons(document, data)
    assert set(results) == {"core", "pelican"}
    for key, result in results.items():
        assert result["eligible"] is True, result["blockers"]
        assert len(result["blocks"]) == 3
        assert sum(len(row["run_ids"]) for row in result["blocks"]) == 6
        expected_ids = set(document["bundles"][key]["left"] + document["bundles"][key]["right"])
        assert {rid for row in result["blocks"] for rid in row["run_ids"]} == expected_ids
    assert results["core"]["verdicts"]["e2e_ms"]["verdict"] == "lmstudio"
    assert results["pelican"]["verdicts"]["e2e_ms"]["verdict"] == "llama-cpp"
