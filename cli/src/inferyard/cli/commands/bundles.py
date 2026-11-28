"""CLI subcommands for inspecting and verifying benchmark bundles."""

from pathlib import Path


def add_commands(commands):
    parser = commands.add_parser("bundles", help="list or verify benchmark bundles")
    actions = parser.add_subparsers(required=True)

    listed = actions.add_parser("list", help="list registered benchmark bundles and metadata")
    listed.set_defaults(command="bundles list")

    verify = actions.add_parser(
        "verify", help="verify registered benchmarks/ or a local bundle JSON file/directory"
    )
    verify.set_defaults(command="bundles verify")
    verify.add_argument(
        "bundle_target",
        nargs="?",
        type=Path,
        default=None,
        help="optional local bundle JSON file or directory; omit to verify benchmarks/",
    )
