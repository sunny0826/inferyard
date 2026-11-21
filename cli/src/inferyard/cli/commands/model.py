"""Lightweight model preparation arguments; no network backend imports."""

from pathlib import Path


def add_commands(commands):
    model = commands.add_parser("model", help="acquire one pinned remote GGUF into a new directory")
    actions = model.add_subparsers(required=True)
    acquire = actions.add_parser(
        "acquire", help="download one pinned HF/ModelScope GGUF like curl -O"
    )
    acquire.set_defaults(command="model acquire")
    acquire.add_argument("--source", dest="model_source_url", required=True)
    acquire.add_argument("--out", type=Path, required=True)
    acquire.add_argument(
        "--token-env", dest="model_token_env", help="environment variable name only"
    )
    acquire.add_argument(
        "--timeout-seconds",
        dest="model_timeout_seconds",
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
    acquire.add_argument(
        "--proxy-origin",
        dest="proxy_origin",
        help="explicit http(s) proxy origin (scheme://host[:port])",
    )
    acquire.add_argument(
        "--kind",
        dest="model_kind",
        choices=("auto", "gguf", "directory"),
        default="auto",
        help="model format to acquire (default: auto)",
    )
    resolve = actions.add_parser(
        "resolve", help="resolve one HF/ModelScope GGUF or directory URL to a pinned revision"
    )
    resolve.set_defaults(command="model resolve")
    resolve.add_argument("--source", dest="model_source_url", required=True)
    resolve.add_argument(
        "--token-env", dest="model_token_env", help="environment variable name only"
    )
    resolve.add_argument(
        "--timeout-seconds",
        dest="model_timeout_seconds",
        type=float,
        default=30,
        help="positive finite seconds per network operation (default: 30)",
    )
    resolve.add_argument(
        "--proxy-origin",
        dest="proxy_origin",
        help="explicit http(s) proxy origin (scheme://host[:port])",
    )
    resolve.add_argument(
        "--kind",
        dest="model_kind",
        choices=("auto", "gguf", "directory"),
        default="auto",
        help="model format to resolve (default: auto)",
    )
