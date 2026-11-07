"""Internal engine-comparison.v2 run set, handoff chain and history stop (ADR 057c)."""

from collections.abc import Callable
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.analysis.comparison_types import JSONRecord
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
    SERVICE_IDENTITY_INVALID,
    SERVICE_IDENTITY_MISSING,
    SERVICE_NOT_REPLACED,
    UNPLANNED_TRIAL,
    bundle_comparisons,
    comparison_history_stop,
    evaluate_comparison,
)
from inferyard.config.comparison_plan import Slot, comparison_slots, freeze
from tests.unit.test_comparison_plan import comparison_inputs
from tests.unit.test_engine_blocks import block, request


def plan_with_slots(tmp_path: Path) -> tuple[JSONRecord, tuple[Slot, ...]]:
    left, right, specs = comparison_inputs(tmp_path)
    plan = freeze(
        left,
        right,
        specs,
        experiment_id="engine-compare-v2-fixture",
        destination=tmp_path / "plan",
    )
    return plan, comparison_slots(plan)


def run_id(slot: Slot) -> str:
    return f"run-{slot.block}-{slot.side}-{slot.bundle_key}"


def synthetic_runs(plan: JSONRecord, *, same_plan: bool = True) -> list[JSONRecord]:
    slots = comparison_slots(plan)
    entries, previous = [], None
    for index, slot in enumerate(slots):
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
                "identity": {
                    "local_engine": {
                        "binding": {"service": {"pid": 1000 + index, "start_ticks": 2000 + index}}
                    }
                },
                "service_binding": {"previous_run_id": previous},
            }
        )
        previous = identifier
    return entries


def tmp_path_path(slot: Slot) -> str:
    return f"/runs/{slot.trial_id}"


def reasons(document: JSONRecord) -> list[str]:
    return [row["reason"] for row in document["diagnostics"]]


def test_complete_frozen_chain_is_eligible(tmp_path: Path) -> None:
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


def test_missing_run_is_reported_once(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    del runs[5]
    document = evaluate_comparison(plan, runs)
    assert PLANNED_RUN_MISSING in reasons(document)
    assert [row["trial_id"] for row in document["run_set"]["missing"]] == [
        plan["trials"][5]["trial_id"]
    ]
    # A gap with later runs is an illegal subsequent run, not an empty tail.
    assert document["history_stop"]["reason"] == HISTORY_IDENTITY_ERROR
    assert document["history_stop"]["detail"] == "subsequent_run_present"


def test_unplanned_trial_uses_the_canonical_reason(tmp_path: Path) -> None:
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


def test_run_with_another_plan_hash_is_mismatched(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[3]["run"]["plan_sha256"] = "f" * 64
    document = evaluate_comparison(plan, runs)
    assert reasons(document) == [RUN_PLAN_MISMATCH]


def test_duplicate_run_id_direct_entry_is_classified(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs.append(deepcopy(runs[0]))
    document = evaluate_comparison(plan, runs)
    assert "comparison_duplicate_trial_attempt" in reasons(document)
    assert document["run_set"]["duplicates"][0]["detail"] == "duplicate_run_id"
    assert document["eligible"] is False


def test_duplicate_run_id_across_trials_rejects_both(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    duplicate = deepcopy(runs[0])
    duplicate["run"]["trial_id"] = runs[4]["run"]["trial_id"]
    duplicate["path"] = "/runs/cross-trial-copy"
    runs.append(duplicate)
    document = evaluate_comparison(plan, runs)
    rows = [
        row
        for row in document["run_set"]["duplicates"]
        if row["detail"] == "duplicate_run_id"
    ]
    assert {row["trial_id"] for row in rows} == {
        plan["trials"][0]["trial_id"],
        plan["trials"][4]["trial_id"],
    }
    assert {row["run_id"] for row in rows} == {runs[0]["run"]["run_id"]}
    assert {row["path"] for row in rows} == {runs[0]["path"], duplicate["path"]}
    selected = {
        row["run_id"]
        for row in document["handoff"]["slots"]
        if row["run_id"] is not None
    }
    assert runs[0]["run"]["run_id"] not in selected
    assert runs[4]["run"]["run_id"] not in selected
    assert document["eligible"] is False


def test_duplicate_trial_attempt_blocks(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    duplicate = deepcopy(runs[0])
    duplicate["run"]["run_id"] = "run-resumed"
    runs.append(duplicate)
    document = evaluate_comparison(plan, runs)
    assert "comparison_duplicate_trial_attempt" in reasons(document)
    assert document["run_set"]["duplicates"][0]["trial_id"] == plan["trials"][0]["trial_id"]


def test_first_slot_with_predecessor_is_rejected(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[0]["service_binding"]["previous_run_id"] = "run-before"
    document = evaluate_comparison(plan, runs)
    assert reasons(document) == [HANDOFF_SLOT_MISMATCH]


def test_missing_sealed_binding_is_rejected(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[1]["service_binding"] = None
    document = evaluate_comparison(plan, runs)
    assert reasons(document) == [HANDOFF_MISSING]


def test_empty_expected_predecessor_is_rejected(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[2]["service_binding"]["previous_run_id"] = None
    document = evaluate_comparison(plan, runs)
    assert reasons(document) == [HANDOFF_MISSING]


@pytest.mark.parametrize("index", [2, 3])
def test_chain_must_point_to_the_previous_slot(tmp_path: Path, index: int) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[index]["service_binding"]["previous_run_id"] = runs[0]["run"]["run_id"]
    document = evaluate_comparison(plan, runs)
    assert reasons(document) == [HANDOFF_BROKEN]


def test_same_plan_across_batches_cannot_splice_slots(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)[:2]
    runs[1]["service_binding"]["previous_run_id"] = None
    document = evaluate_comparison(plan, runs)
    assert HANDOFF_MISSING in reasons(document)
    assert document["eligible"] is False


def test_reader_order_and_event_time_never_override_plan_order(tmp_path: Path) -> None:
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
def test_terminal_history_blocks_further_slots(
    tmp_path: Path, mutate: Callable[[list[JSONRecord]], None], expected: str
) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    mutate(runs)
    document = evaluate_comparison(plan, runs)
    assert expected in reasons(document)
    assert document["history_stop"]["reason"] == expected
    assert document["eligible"] is False


def test_subsequent_run_after_a_stop_is_reported(tmp_path: Path) -> None:
    plan, slots = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[1]["summary"]["stop_reason"] = "user_cancelled"
    stop = comparison_history_stop(plan, runs)
    assert stop["reason"] == HISTORY_CANCELLED
    assert stop["trial_id"] == slots[1].trial_id
    assert runs[2]["run"]["run_id"] in stop["subsequent_run_ids"]


def test_earlier_cancel_wins_over_a_later_gap(tmp_path: Path) -> None:
    plan, slots = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[0]["summary"]["stop_reason"] = "user_cancelled"
    del runs[1]
    stop = comparison_history_stop(plan, runs)
    assert stop["trial_id"] == slots[0].trial_id
    assert stop["run_id"] == runs[0]["run"]["run_id"]
    assert stop["reason"] == HISTORY_CANCELLED
    assert stop["detail"] == "user_cancelled"
    assert runs[0]["run"]["run_id"] not in stop["subsequent_run_ids"]
    assert set(stop["subsequent_run_ids"]) == {
        entry["run"]["run_id"] for entry in runs[1:]
    }


def test_normal_handoff_and_quality_outcomes_do_not_stop(tmp_path: Path) -> None:
    plan, slots = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)[:2]
    assert comparison_history_stop(plan, runs) is None
    runs = synthetic_runs(plan)
    for entry in runs:
        for row in entry["requests"]:
            row["quality_state"] = "unscorable"
    assert comparison_history_stop(plan, runs) is None


def test_resume_attempt_and_later_run_after_gap_are_identity_errors(tmp_path: Path) -> None:
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


def bundle_block_data(slots: tuple[Slot, ...]) -> dict[str, JSONRecord]:
    data = {}
    for key, right_ms in (("core", 800), ("pelican", 1200)):
        for slot in (item for item in slots if item.bundle_key == key):
            data[run_id(slot)] = block_row(slot, key, right_ms)
    return data


def block_row(slot: Slot, key: str, right_ms: int) -> JSONRecord:
    order = slot.block - 1
    family = "llama-cpp" if slot.side == "a" else "lmstudio"
    row = block(family, order, e2e_ms=right_ms if slot.side == "b" else 1000)
    row["run"].update(run_id=run_id(slot), trial_id=slot.trial_id)
    row["plan"]["trials"] = [{"trial_id": slot.trial_id, "workload_id": "w"}]
    row["selection"].update(bundle_sha256=key, case_ids=["case"])
    row["requests"] = [
        request(index, e2e_ms=right_ms if slot.side == "b" else 1000) for index in range(2)
    ]
    return row


def test_per_bundle_blocks_use_the_existing_six_block_verdict(tmp_path: Path) -> None:
    plan, slots = plan_with_slots(tmp_path)
    data = bundle_block_data(slots)
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


@pytest.mark.parametrize(
    ("mutate", "expected"),
    [
        (lambda runs: runs[1].pop("identity"), SERVICE_IDENTITY_MISSING),
        (
            lambda runs: runs[1]["identity"]["local_engine"]["binding"]["service"].update(
                pid="1001"
            ),
            SERVICE_IDENTITY_INVALID,
        ),
        (
            lambda runs: runs[1]["identity"]["local_engine"]["binding"].update(
                service=deepcopy(runs[0]["identity"]["local_engine"]["binding"]["service"])
            ),
            SERVICE_NOT_REPLACED,
        ),
        (
            lambda runs: runs[6]["identity"]["local_engine"]["binding"].update(
                service=deepcopy(runs[0]["identity"]["local_engine"]["binding"]["service"])
            ),
            SERVICE_NOT_REPLACED,
        ),
    ],
)
def test_process_identity_must_be_present_and_unique_across_all_slots(
    tmp_path: Path, mutate: Callable[[list[JSONRecord]], None], expected: str
) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    mutate(runs)
    document = evaluate_comparison(plan, runs)
    assert expected in reasons(document)
    assert document["eligible"] is False


def test_overall_blocker_removes_every_bundle_qualification(tmp_path: Path) -> None:
    plan, slots = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[2]["service_binding"]["previous_run_id"] = runs[0]["run"]["run_id"]
    document = evaluate_comparison(plan, runs)
    assert document["eligible"] is False
    results = bundle_comparisons(document, {})
    for result in results.values():
        assert result["eligible"] is False
        assert "comparison_overall_blocked" in result["blockers"]
        assert {row["verdict"] for row in result["verdicts"].values()} == {"not_eligible"}
        assert result["blocks"] == []


def test_frozen_pairing_ignores_event_time_for_v2(tmp_path: Path) -> None:
    plan, slots = plan_with_slots(tmp_path)
    data = bundle_block_data(slots)
    document = evaluate_comparison(plan, synthetic_runs(plan))
    baseline = bundle_comparisons(document, data)
    # Swap event times between two right-side blocks: frozen order must still decide.
    first = data[document["bundles"]["core"]["blocks"][1]["right_run_id"]]
    second = data[document["bundles"]["core"]["blocks"][2]["right_run_id"]]
    first["first_event_utc"], second["first_event_utc"] = (
        second["first_event_utc"],
        first["first_event_utc"],
    )
    swapped = bundle_comparisons(document, data)
    assert swapped["core"]["verdicts"] == baseline["core"]["verdicts"]
    assert [row["run_ids"] for row in swapped["core"]["blocks"]] == [
        row["run_ids"] for row in baseline["core"]["blocks"]
    ]


def test_first_real_stop_survives_a_missing_tail(tmp_path: Path) -> None:
    plan, slots = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)[:1]
    runs[0]["summary"]["stop_reason"] = "user_cancelled"
    document = evaluate_comparison(plan, runs)
    assert document["history_stop"]["trial_id"] == slots[0].trial_id
    assert document["history_stop"]["reason"] == HISTORY_CANCELLED
    # Twelve slots minus the first cancelled slot: tails are not called missing.
    assert document["run_set"]["missing"] == []


def test_reader_maps_a_plan_without_required_top_level_shape(tmp_path: Path) -> None:
    from inferyard.analysis.engine_comparison_v2 import read_comparison_v2
    from inferyard.evidence.storage import json_bytes

    (tmp_path / "bad").mkdir()
    (tmp_path / "bad" / "plan.json").write_bytes(json_bytes({"schema_version": 3}))
    document = read_comparison_v2(tmp_path / "bad" / "plan.json", tmp_path / "batch")
    assert document["diagnostics"][0]["reason"] == "comparison_plan_required"
    assert document["diagnostics"][0]["error"]["type"] == "KeyError"


def test_top_level_reader_rejects_broken_inputs(tmp_path: Path) -> None:
    from inferyard.analysis.engine_comparison_v2 import read_comparison_v2

    missing_plan = read_comparison_v2(tmp_path / "absent", tmp_path / "batch")
    assert missing_plan["diagnostics"][0]["reason"] == "comparison_plan_required"
    assert missing_plan["eligible"] is False

    plan, _ = plan_with_slots(tmp_path)
    batch = tmp_path / "batch"
    batch.mkdir()
    no_batch = read_comparison_v2(tmp_path / "plan", batch)
    assert no_batch["diagnostics"][0]["reason"] == "comparison_history_identity_error"
    assert no_batch["diagnostics"][0]["detail"] == "batch_envelope_invalid"

    from inferyard.evidence.storage import json_bytes

    (batch / "batch.json").write_bytes(json_bytes([]))
    invalid_batch = read_comparison_v2(tmp_path / "plan", batch)
    assert invalid_batch["diagnostics"][0]["detail"] == "batch_envelope_invalid"

    (batch / "batch.json").write_bytes(
        json_bytes(
            {
                "schema_version": 3,
                "plan_sha256": plan["plan_sha256"],
                "diagnostic": False,
                "tool_source_sha256": "0" * 64,
            }
        )
    )
    no_runs = read_comparison_v2(tmp_path / "plan", batch)
    assert no_runs["diagnostics"][0]["detail"] == "batch_runs_invalid"
