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
    for option, field, default in (
        ("timeout", "model_timeout_seconds", 1800),
        ("read-timeout", "model_read_timeout_seconds", 180),
        ("cleanup-timeout", "model_cleanup_timeout_seconds", 60),
    ):
        acquire.add_argument(
            f"--{option}-seconds",
            dest=field,
            type=float,
            default=default,
            help=f"positive finite seconds (default: {default})",
        )
