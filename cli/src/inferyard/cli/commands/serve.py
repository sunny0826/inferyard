"""Lightweight standalone serve command registration; no live backend imports."""

from pathlib import Path


def add_commands(commands):
    serve = commands.add_parser("serve", help="prepare standalone local model serving plans")
    actions = serve.add_subparsers(required=True)
    prepare = actions.add_parser(
        "prepare",
        help="generate serve-plan.json for starting a local OpenAI-compatible model service",
    )
    prepare.set_defaults(command="serve prepare")
    prepare.add_argument("--model", dest="model_path", type=Path, required=True)
    engine = prepare.add_mutually_exclusive_group(required=True)
    engine.add_argument("--installation", type=Path)
    engine.add_argument("--engine", dest="engine_path", type=Path)
    engine.add_argument("--runtime-receipt", type=Path)
    prepare.add_argument("--context-size", type=int, required=True)
    prepare.add_argument("--port", type=int, default=48857)
    prepare.add_argument("--out", type=Path, required=True)
    prepare.add_argument(
        "--json",
        action="store_const",
        const="json",
        default="json",
        dest="device_format",
        help="emit JSON result (default)",
    )
