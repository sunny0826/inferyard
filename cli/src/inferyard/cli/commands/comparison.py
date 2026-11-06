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
        "--mode", dest="comparison_mode", choices=("model", "config", "side-by-side", "engine")
    )
    engines = commands.add_parser(
        "compare-engines", help="judge interleaved same-GGUF engine blocks offline"
    )
    engines.add_argument("--left-runs", type=Path, nargs="+", required=True)
    engines.add_argument("--right-runs", type=Path, nargs="+", required=True)
    engines.add_argument("--out", type=Path, required=True)
