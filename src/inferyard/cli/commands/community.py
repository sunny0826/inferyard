"""Installed preparation commands; registration never imports live platform backends."""

from pathlib import Path


def add_commands(commands):
    init = commands.add_parser(
        "init", help="export an offline starter workspace into a new directory"
    )
    init.add_argument("--out", type=Path, required=True)
    init.add_argument(
        "--bundle",
        dest="community_bundle",
        default="zh-smoke",
        choices=("zh-smoke", "zh-core", "zh-svg-pelican"),
    )
    runtime = commands.add_parser("runtime", help="prepare existing, pinned local engine archives")
    actions = runtime.add_subparsers(required=True)
    prepare = actions.add_parser(
        "prepare",
        help="Windows only: verify/extract local archives; runs --version (10s); no download",
    )
    prepare.set_defaults(command="runtime prepare")
    prepare.add_argument("--profile", dest="runtime_profile", required=True)
    prepare.add_argument("--archive", type=Path, required=True)
    prepare.add_argument("--runtime-archive", type=Path)
    prepare.add_argument("--out", type=Path, required=True)
    config = commands.add_parser(
        "config", help="prepare assets, create candidates or bind a service"
    )
    actions = config.add_subparsers(required=True)
    assets = actions.add_parser(
        "assets", help="Linux: export GGUF template and local file identities"
    )
    assets.set_defaults(command="config assets")
    assets.add_argument("--model", dest="model_path", type=Path, required=True)
    assets.add_argument("--engine", dest="engine_path", type=Path, required=True)
    assets.add_argument("--out", type=Path, required=True)
    create = actions.add_parser(
        "create", help="macOS/Windows: create an unbound candidate; runs engine --version (10s)"
    )
    create.set_defaults(command="config create")
    create.add_argument("--preflight", type=Path, required=True)
    create.add_argument("--bundle", dest="bundle_path", type=Path, required=True)
    create.add_argument("--results", dest="output_root", type=Path, required=True)
    engine = create.add_mutually_exclusive_group(required=True)
    engine.add_argument("--engine", dest="engine_path", type=Path)
    engine.add_argument("--runtime-receipt", type=Path)
    create.add_argument("--out", type=Path, required=True)
    create.add_argument("--port", type=int, default=48857)
    create.add_argument("--model-repo", default="local")
    create.add_argument("--model-revision")
    bind = actions.add_parser(
        "bind", help="bind an already running external service without requests"
    )
    bind.set_defaults(command="config bind")
    bind.add_argument("--candidate", type=Path, required=True)
    bind.add_argument("--pid", dest="server_pid", type=int, required=True)
    bind.add_argument("--endpoint", dest="endpoint_url", required=True)
    bind.add_argument("--out", type=Path, required=True)
