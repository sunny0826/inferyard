"""Load batch history with the comparison stop shared by readers and execution."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Protocol

from inferyard.analysis.comparison_history import comparison_history_stop
from inferyard.analysis.comparison_types import JSONRecord, JSONValue
from inferyard.config.loader import LoadedConfig
from inferyard.runtime.batch_state import HistoryOptions, history_diagnostics


class HistoryLoader(Protocol):
    def __call__(
        self,
        root: Path,
        plan: JSONRecord,
        loaded: dict[str, LoadedConfig],
        **options: JSONValue,
    ) -> list[JSONRecord]: ...


@dataclass(frozen=True, slots=True)
class HistoryLoadOptions:
    source_identity: str
    implementation_identity: JSONRecord
    history_loader: HistoryLoader


def load_plan_history(
    root: Path,
    plan: JSONRecord,
    loaded: dict[str, LoadedConfig],
    *,
    options: HistoryLoadOptions,
) -> tuple[list[JSONRecord], JSONRecord | None]:
    from inferyard.analysis.engine_comparison_v2 import is_comparison_plan

    if not is_comparison_plan(plan):
        return (
            options.history_loader(
                root,
                plan,
                loaded,
                source_identity=options.source_identity,
                implementation_identity=options.implementation_identity,
            ),
            None,
        )
    runs, rejections = history_diagnostics(
        root,
        plan,
        loaded,
        HistoryOptions(
            source_identity=options.source_identity,
            implementation_identity=options.implementation_identity,
            full_verification=False,
        ),
    )
    return runs, comparison_history_stop(plan, runs, rejections)
