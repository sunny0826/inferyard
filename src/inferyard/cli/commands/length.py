"""Argument registration for length commands."""

from pathlib import Path


def add_commands(commands):
    length = commands.add_parser(
        "prepare-length", help="construct measured model-specific performance text"
    )
    length.add_argument("--config", type=Path, required=True)
    length.add_argument("--spec", dest="length_spec", type=Path, required=True)
    length.add_argument("--endpoint-url")
    length.add_argument("--server-pid", type=int)
    length.add_argument("--out", type=Path, required=True)
