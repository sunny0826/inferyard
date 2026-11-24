"""Internal engine-comparison.v2 run set, handoff chain and history stop."""

from collections.abc import Callable
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.analysis.comparison_history import (
    HISTORY_CANCELLED,
    HISTORY_FAILED,
    HISTORY_INCOMPLETE,
)
from inferyard.analysis.engine_comparison_v2 import (
    SERVICE_IDENTITY_INVALID,
    SERVICE_IDENTITY_MISSING,
    SERVICE_NOT_REPLACED,
    evaluate_comparison,
)
from inferyard.config.comparison_plan import Slot, comparison_slots, freeze
from inferyard.contracts.comparison_types import JSONRecord
from tests.unit.test_comparison_plan import comparison_inputs


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
                "path": f"/runs/{slot.trial_id}",
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


def reasons(document: JSONRecord) -> list[str]:
    return [row["reason"] for row in document["diagnostics"]]


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
