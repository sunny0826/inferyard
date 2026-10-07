"""Argument registration for migration commands."""

from pathlib import Path


def add_commands(commands):
    migration = commands.add_parser(
        "migrate", help="migrate sealed legacy evidence to a new directory"
    )
    migration.add_argument("--run", type=Path, required=True)
    migration.add_argument("--out", type=Path, required=True)
