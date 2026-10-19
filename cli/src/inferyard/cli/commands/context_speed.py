"""Offline context speed result command registration."""

from pathlib import Path


def add_commands(commands):
    command = commands.add_parser("speed-results", help="read verified context speed evidence")
    command.add_argument("--batch", type=Path, required=True)
