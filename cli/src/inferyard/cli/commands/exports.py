"""Argument registration for exports commands."""

from pathlib import Path


def add_commands(commands):
    export = commands.add_parser("export", help="export analysis as JSON, CSV and Markdown")
    source = export.add_mutually_exclusive_group(required=True)
    source.add_argument("--run", type=Path)
    source.add_argument("--analysis", dest="analysis_path", type=Path)
    export.add_argument("--out", type=Path, required=True)
