"""Lightweight model preparation arguments; no network backend imports."""

from pathlib import Path


def add_commands(commands):
    model = commands.add_parser("model", help="acquire one pinned remote GGUF into a new directory")
    actions = model.add_subparsers(required=True)
    acquire = actions.add_parser(
        "acquire", help="verify and download one immutable HF/ModelScope GGUF"
    )
    acquire.set_defaults(command="model acquire")
    acquire.add_argument("--source", dest="model_source_url", required=True)
    acquire.add_argument("--out", type=Path, required=True)
    acquire.add_argument(
        "--token-env", dest="model_token_env", help="environment variable name only"
    )
