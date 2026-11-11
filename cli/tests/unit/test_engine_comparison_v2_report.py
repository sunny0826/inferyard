"""Public v2 derivation entry rejects legacy left/right inputs."""

from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.errors import ContractError


def test_compare_engines_parser_accepts_plan_and_batch_only(tmp_path: Path) -> None:
    from inferyard.cli.arguments import parser

    args = parser().parse_args(
        [
            "compare-engines",
            "--plan",
            str(tmp_path / "plan.json"),
            "--batch",
            str(tmp_path / "batch"),
            "--out",
            str(tmp_path / "out"),
        ]
    )
    assert args.frozen_plan == tmp_path / "plan.json"
    assert args.batch == tmp_path / "batch"
    from inferyard.cli.arguments import ArgumentError

    with pytest.raises(ArgumentError):
        parser().parse_args(
            ["compare-engines", "--left-runs", "a", "--right-runs", "b", "--out", "out"]
        )


def test_compare_engines_requires_plan_and_batch(tmp_path: Path) -> None:
    from inferyard.reporting.engine_comparison_v2_report import execute

    with pytest.raises(ContractError):
        execute(CommandRequest("compare-engines", out=tmp_path / "out"))
