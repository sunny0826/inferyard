"""Argument registration for public commands."""

from pathlib import Path

from inferyard.cli.commands import compatibility_parser


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
    legacy_package = compatibility_parser(
        commands, "public-package", "create a redacted local candidate summary"
    )
    _package_arguments(legacy_package)
    public_check = compatibility_parser(
        commands, "public-check", "verify public package file integrity"
    )
    public_check.add_argument("--run", type=Path, required=True)
    public_check.add_argument("--from-run", type=Path)
    portable = compatibility_parser(
        commands, "public-config-check", "check local declared configuration against public package"
    )
    portable.add_argument("--run", type=Path, required=True)
    portable.add_argument("--config", type=Path, required=True)
    public_plan = compatibility_parser(
        commands, "public-plan", "freeze all repeats of a public candidate workload offline"
    )
    _plan_arguments(public_plan)
