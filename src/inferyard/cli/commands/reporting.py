"""Argument registration for reporting commands."""

from pathlib import Path


def add_commands(commands):
    report = commands.add_parser("report", help="render saved trials as an offline report")
    report.add_argument("--runs", dest="report_runs", nargs="+", type=Path, required=True)
    report.add_argument("--out", required=True, type=Path)
    report.add_argument("--comparison", dest="comparison_path", type=Path)
