"""Argument registration for reporting commands."""

from pathlib import Path


def add_commands(commands):
    report = commands.add_parser("report", help="write saved trials as one Studio JSON")
    report.add_argument("--runs", dest="report_runs", nargs="+", type=Path, required=True)
    report.add_argument("--out", required=True, type=Path)
