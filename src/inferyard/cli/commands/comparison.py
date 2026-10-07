"""Argument registration for comparison commands."""

from pathlib import Path

from inferyard.cli.commands import compatibility_parser


def add_commands(commands):
    comparison_check = compatibility_parser(commands, "compare-check", "recompute saved comparison")
    comparison_check.add_argument("--run", type=Path, required=True)
    comparison_check.add_argument(
        "--source-root", dest="source_roots", action="append", metavar="OLD=NEW"
    )
    comparison = commands.add_parser("compare", help="compare frozen trial evidence offline")
    comparison.add_argument("--left", type=Path, required=True)
    comparison.add_argument("--right", type=Path, required=True)
    comparison.add_argument("--out", type=Path, required=True)
    comparison.add_argument("--left-overhead", type=Path)
    comparison.add_argument("--right-overhead", type=Path)
    comparison.add_argument(
        "--mode", dest="comparison_mode", choices=("model", "config", "side-by-side")
    )
