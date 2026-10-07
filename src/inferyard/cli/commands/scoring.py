"""Argument registration for scoring commands."""

from pathlib import Path

from inferyard.cli.commands import compatibility_parser


def add_commands(commands):
    rescore = commands.add_parser("rescore", help="rescore saved answers into a separate analysis")
    rescore.add_argument("--run", type=Path, required=True)
    rescore.add_argument("--out", type=Path, required=True)
    rescore.add_argument("--scorer", dest="scorer_id", required=True)
    rescore.add_argument("--reason", dest="revision_reason", required=True)
    rescore.add_argument("--parent-analysis", dest="analysis_path", type=Path)
    rescore_check = compatibility_parser(
        commands, "rescore-check", "replay a scoring revision offline"
    )
    rescore_check.add_argument("--run", type=Path, required=True)
