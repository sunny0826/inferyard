"""Argument registration for repetition commands."""

from pathlib import Path

from inferyard.cli.commands import compatibility_parser


def add_commands(commands):
    repeats = commands.add_parser("repeat-summary", help="derive frozen repeat consistency offline")
    repeats.add_argument("--run", type=Path, required=True)
    repeats.add_argument("--out", type=Path, required=True)
    repeat_check = compatibility_parser(
        commands, "repeat-check", "verify repeat analyses against source evidence"
    )
    repeat_check.add_argument("--run", type=Path, required=True)
