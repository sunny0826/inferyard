"""Argument registration for repetition commands."""

from pathlib import Path


def add_commands(commands):
    repeats = commands.add_parser("repeat-summary", help="derive frozen repeat consistency offline")
    repeats.add_argument("--run", type=Path, required=True)
    repeats.add_argument("--out", type=Path, required=True)
