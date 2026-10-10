"""Argument registration for discovery commands."""

from pathlib import Path


def add_commands(commands):
    device = commands.add_parser(
        "device-check", help="inspect macOS/Linux/Windows hardware (human output by default)"
    )
    device.add_argument("--models-root", action="append", type=Path, dest="models_roots")
    device.add_argument(
        "--model", type=Path, dest="model_path", help="inspect an existing local GGUF"
    )
    device.add_argument(
        "--out", type=Path, help="save JSON to a new directory, including blocked results"
    )
    output_format = device.add_mutually_exclusive_group()
    output_format.add_argument(
        "--format",
        choices=("human", "json"),
        default="human",
        dest="device_format",
        help="stdout format (default: human)",
    )
    output_format.add_argument(
        "--json",
        action="store_const",
        const="json",
        dest="device_format",
        help="Agent output; alias for --format json",
    )
    catalogue = commands.add_parser("catalogue", help="list methods or proposed metric definitions")
    catalogue.add_argument(
        "--kind", dest="catalogue_kind", choices=("methods", "metrics", "components"), required=True
    )
    plan = commands.add_parser("plan", help="preview or freeze an offline experiment plan")
    plan.add_argument("--experiment", type=Path, required=True)
    output = plan.add_mutually_exclusive_group(required=True)
    output.add_argument("--dry-run", action="store_true")
    output.add_argument("--out", type=Path)
