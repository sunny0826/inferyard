"""Argument registration for comparison commands."""

from pathlib import Path


def add_commands(commands):
    comparison = commands.add_parser("compare", help="compare frozen trial evidence offline")
    comparison.add_argument("--left", type=Path, required=True)
    comparison.add_argument("--right", type=Path, required=True)
    comparison.add_argument("--out", type=Path, required=True)
    comparison.add_argument("--left-overhead", type=Path)
    comparison.add_argument("--right-overhead", type=Path)
    comparison.add_argument(
        "--mode", dest="comparison_mode", choices=("model", "config", "side-by-side")
    )
