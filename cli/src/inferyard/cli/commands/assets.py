"""Lightweight local asset scanning arguments; no live backend imports."""

from pathlib import Path


def add_commands(commands):
    """Register the read-only `assets scan` subcommand."""
    assets = commands.add_parser("assets", help="inspect local model and engine assets read-only")
    actions = assets.add_subparsers(required=True)
    scan = actions.add_parser("scan", help="scan local model and engine directories")
    scan.set_defaults(command="assets scan")
    scan.add_argument("--models-root", action="append", type=Path, dest="models_roots")
    scan.add_argument("--engine-root", action="append", type=Path, dest="engine_roots")
