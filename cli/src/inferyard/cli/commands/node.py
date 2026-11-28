"""Lightweight remote node command registration; no live backend imports."""

from pathlib import Path


def _json(parser) -> None:
    parser.add_argument(
        "--json",
        action="store_const",
        const="json",
        default="json",
        dest="device_format",
        help="emit JSON result (default)",
    )


def _state(parser) -> None:
    parser.add_argument("--state-dir", type=Path, help="node credential and runtime state directory")


def add_commands(commands) -> None:
    join = commands.add_parser("join", help="pair this machine with an InferYard Studio hub")
    join.set_defaults(command="join")
    join.add_argument("--hub", dest="hubs", action="append", required=True)
    join.add_argument("--code", required=True, help="six-digit one-time pairing code")
    join.add_argument("--ca-sha256", required=True, help="pinned root CA SPKI SHA-256 fingerprint")
    _state(join)
    join.add_argument("--no-autostart", action="store_true")
    join.add_argument("--source-hint", choices=("Tailscale", "LAN", "SSH"))
    _json(join)

    agent = commands.add_parser("agent", help="run or manage the paired InferYard node agent")
    actions = agent.add_subparsers(required=True)
    run = actions.add_parser("run", help="run the resident mTLS control agent")
    run.set_defaults(command="agent run")
    _state(run)
    run.add_argument("--hub", dest="hubs", action="append")
    _json(run)
    for name in ("install-autostart", "uninstall-autostart"):
        action = actions.add_parser(name, help=f"{name.replace('-', ' ')} for this user")
        action.set_defaults(command=f"agent {name}")
        _state(action)
        _json(action)
