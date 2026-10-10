"""Argument registration for public commands."""

from pathlib import Path


def _package_arguments(command):
    command.add_argument("--run", type=Path, required=True)
    command.add_argument("--out", type=Path, required=True)


def _plan_arguments(command):
    _package_arguments(command)
    command.add_argument("--config", type=Path, required=True)


def add_commands(commands):
    public = commands.add_parser("public", help="package or plan a public candidate offline")
    actions = public.add_subparsers(dest="public_action", required=True)
    package = actions.add_parser("package", help="create a redacted local candidate summary")
    package.set_defaults(command="public package")
    _package_arguments(package)
    plan = actions.add_parser("plan", help="freeze all repeats of a public candidate offline")
    plan.set_defaults(command="public plan")
    _plan_arguments(plan)
