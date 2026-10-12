"""Lightweight engine preparation arguments; no network backend imports."""

from pathlib import Path


def add_commands(commands):
    engine = commands.add_parser(
        "engine", help="acquire one pinned engine archive into a new directory"
    )
    actions = engine.add_subparsers(required=True)
    acquire = actions.add_parser(
        "acquire", help="download one engine file like curl -O"
    )
    acquire.set_defaults(command="engine acquire")
    acquire.add_argument("--source", dest="engine_source_url", required=True)
    acquire.add_argument("--out", type=Path, required=True)
    acquire.add_argument(
        "--timeout-seconds",
        dest="engine_timeout_seconds",
        type=float,
        default=1800,
        help="positive finite seconds per network operation (default: 1800)",
    )
