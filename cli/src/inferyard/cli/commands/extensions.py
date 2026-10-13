"""Register extension commands without importing their execution workflows."""

from pathlib import Path


def _arguments(command, action):
    if action == "freeze":
        command.add_argument("--spec", dest="extension_spec", type=Path, required=True)
    else:
        canonical = "--plan" if action == "run" else "--packet"
        command.add_argument(canonical, dest="extension_spec", type=Path, required=True)
    command.add_argument("--out", type=Path, required=True)
    if action != "replay":
        command.add_argument("--config", type=Path, required=True)


def add_commands(commands):
    extension = commands.add_parser("extension", help="freeze, run or replay an extension protocol")
    actions = extension.add_subparsers(dest="extension_action", required=True)
    for action in ("freeze", "run", "replay"):
        command = actions.add_parser(action, help=f"{action} the separate extension protocol")
        command.set_defaults(command=f"extension {action}")
        _arguments(command, action)
