"""Argument registration for local engine detection and installation scaffolding."""

from pathlib import Path

from inferyard.contracts.engine_profiles import LOCAL_FAMILIES


def add_detect_subcommands(actions):
    detect = actions.add_parser(
        "detect",
        help="read-only detection of local engine binaries and Python venvs",
    )
    detect.set_defaults(command="engine detect")
    detect.add_argument(
        "--extra-path",
        action="append",
        dest="extra_paths",
        type=Path,
        help="additional executable, venv, or directory to inspect",
    )

    init = actions.add_parser(
        "init-installation",
        help="generate and validate a local-engine-installation.v1 JSON file",
    )
    init.set_defaults(command="engine init-installation")
    init.add_argument("--family", choices=LOCAL_FAMILIES, required=True)
    init.add_argument("--executable", type=Path, required=True)
    init.add_argument("--out", type=Path, required=True)
    init.add_argument("--working-directory", type=Path)
    init.add_argument(
        "--runtime-root",
        action="append",
        dest="runtime_roots",
        type=Path,
    )
    init.add_argument("--models-root", type=Path)
