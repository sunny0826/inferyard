"""Lightweight engine preparation arguments; no network backend imports."""

from pathlib import Path

from inferyard.cli.commands.engine_detect import add_detect_subcommands


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
    acquire.add_argument(
        "--progress-stream",
        dest="progress_stream",
        action="store_true",
        help="emit single-line JSON transfer progress events on stderr",
    )
    acquire.add_argument(
        "--resume",
        dest="resume_transfer",
        action="store_true",
        help="resume an incomplete .part download when present",
    )
    prepare = actions.add_parser(
        "prepare",
        help="download, verify, and unpack one engine archive into a new directory",
    )
    prepare.set_defaults(command="engine prepare")
    prepare.add_argument("--source", dest="engine_source_url", required=True)
    prepare.add_argument("--out", type=Path, required=True)
    prepare.add_argument("--expected-sha256")
    prepare.add_argument("--profile", dest="runtime_profile")
    prepare.add_argument("--runtime-source", dest="runtime_source_url")
    prepare.add_argument("--runtime-expected-sha256")
    prepare.add_argument(
        "--timeout-seconds",
        dest="engine_timeout_seconds",
        type=float,
        default=1800,
        help="positive finite seconds per network operation (default: 1800)",
    )
    prepare.add_argument(
        "--progress-stream",
        dest="progress_stream",
        action="store_true",
        help="emit single-line JSON transfer progress events on stderr",
    )
    prepare.add_argument(
        "--resume",
        dest="resume_transfer",
        action="store_true",
        help="resume an incomplete .part download when present",
    )
    check = actions.add_parser(
        "check",
        help="macOS/Windows: read-only engine verification; runs --version (30s); no writes",
    )
    check.set_defaults(command="engine check")
    source = check.add_mutually_exclusive_group(required=True)
    source.add_argument("--engine", dest="engine_path", type=Path)
    source.add_argument("--runtime-receipt", type=Path)
    source.add_argument("--installation", type=Path)
    add_detect_subcommands(actions)
