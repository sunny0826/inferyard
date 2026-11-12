"""Bundle grouping for a selected comparison run set."""

from __future__ import annotations

from inferyard.contracts.comparison_types import JSONRecord
from inferyard.config.comparison_plan import Slot


def _bundle_headers(plan: JSONRecord, slots: tuple[Slot, ...]) -> JSONRecord:
    workloads = {item["workload_id"]: item for item in plan["experiment"]["workloads"]}
    groups = {}
    for slot in slots:
        if slot.bundle_key not in groups:
            groups[slot.bundle_key] = {
                "bundle_sha256": workloads[slot.workload_id]["bundle"]["sha256"],
                "left": [],
                "right": [],
                "blocks": [],
            }
    return groups


def _append_side_runs(
    groups: JSONRecord, slots: tuple[Slot, ...], selected: dict[str, JSONRecord]
) -> int:
    block_count = max(slot.block for slot in slots)
    for block in range(1, block_count + 1):
        for slot in slots:
            entry = selected.get(slot.trial_id)
            if slot.block != block or entry is None:
                continue
            side = "left" if slot.side == "a" else "right"
            groups[slot.bundle_key][side].append(entry["run"]["run_id"])
    return block_count


def _block_run(
    selected: dict[str, JSONRecord], slots: tuple[Slot, ...], key: str, block: int
) -> str | None:
    entry = next(
        (
            selected[slot.trial_id]
            for slot in slots
            if slot.bundle_key == key and slot.block == block and slot.trial_id in selected
        ),
        None,
    )
    return None if entry is None else entry["run"]["run_id"]


def _append_bundle_pairs(
    groups: JSONRecord,
    slots: tuple[Slot, ...],
    selected: dict[str, JSONRecord],
    block_count: int,
) -> None:
    for key, group in groups.items():
        for pair in range(1, block_count // 2 + 1):
            left_block, right_block = pair * 2 - 1, pair * 2
            group["blocks"].append(
                {
                    "pair": pair,
                    "left_block": left_block,
                    "right_block": right_block,
                    "left_run_id": _block_run(selected, slots, key, left_block),
                    "right_run_id": _block_run(selected, slots, key, right_block),
                }
            )


def bundle_groups(
    plan: JSONRecord, slots: tuple[Slot, ...], selected: dict[str, JSONRecord]
) -> JSONRecord:
    groups = _bundle_headers(plan, slots)
    block_count = _append_side_runs(groups, slots, selected)
    _append_bundle_pairs(groups, slots, selected, block_count)
    return groups
