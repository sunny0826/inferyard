"""Argument registration for reporting commands."""

from pathlib import Path

from inferyard.cli.commands import compatibility_parser


def add_commands(commands):
    report_check = compatibility_parser(
        commands, "report-check", "rebuild and verify a saved report"
    )
    report_check.add_argument("--run", type=Path, required=True)
    report_check.add_argument(
        "--source-root", dest="source_roots", action="append", metavar="OLD=NEW"
    )
    report_check.add_argument("--rerender", action="store_true")
    report = commands.add_parser("report", help="render saved trials as an offline report")
    reports = report.add_mutually_exclusive_group(required=True)
    reports.add_argument("--runs", dest="report_runs", nargs="+", type=Path)
    reports.add_argument("--run", type=Path, help="compatibility input for a single run")
    report.add_argument("--out", required=True, type=Path)
    report.add_argument("--comparison", dest="comparison_path", type=Path)
