"""Argument registration for candidates commands."""

from pathlib import Path


def add_commands(commands):
    candidates = commands.add_parser(
        "filter-candidates", help="filter saved trials by explicit metric thresholds"
    )
    candidates.add_argument("--spec", dest="filter_spec", type=Path, required=True)
    candidates.add_argument("--out", type=Path, required=True)
