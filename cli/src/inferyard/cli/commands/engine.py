"""Lightweight engine preparation arguments; no network backend imports."""

from pathlib import Path


def add_commands(commands):
    engine = commands.add_parser(
        "engine", help="acquire one pinned engine archive or verify a local engine read-only"
    )
    actions = engine.add_subparsers(required=True)
    acquire = actions.add_parser("acquire", help="download one engine file like curl -O")
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
    check = actions.add_parser(
        "check",
        help="macOS/Windows: read-only engine verification; runs --version (30s); no writes",
    )
    check.set_defaults(command="engine check")
    source = check.add_mutually_exclusive_group(required=True)
    source.add_argument("--engine", dest="engine_path", type=Path)
    source.add_argument("--runtime-receipt", type=Path)
