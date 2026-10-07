"""Sanitized parser construction and lightweight command registration."""

import argparse

from inferyard import __version__
from inferyard.cli.commands import (
    candidates,
    community,
    comparison,
    discovery,
    engine_fit,
    execution,
    exports,
    extensions,
    length,
    migration,
    overhead,
    public,
    repetition,
    reporting,
    scoring,
    verification,
)
from inferyard.contracts.schemas import schemas_for


class ArgumentError(ValueError):
    pass


class Parser(argparse.ArgumentParser):
    def error(self, message):
        # argparse's default diagnostics echo raw arguments, potentially credentials.
        raise ArgumentError("invalid CLI arguments; use --help")


def parser() -> Parser:
    result = Parser(
        prog="inferyard",
        description="Cross-platform local AI benchmark CLI (in development)",
        epilog=(
            "Compatibility entries remain available with their own --help: check, "
            "overhead-check, repeat-check, rescore-check, export-check, public-check, "
            "public-config-check, report-check, compare-check, extension-check, "
            "public-package, public-plan, extension-freeze, extension-run, extension-replay."
        ),
    )
    metadata = result.add_mutually_exclusive_group()
    metadata.add_argument("--version", action="version", version=f"inferyard {__version__}")
    metadata.add_argument("--versions", action="store_true", help="export runtime/dependency JSON")
    metadata.add_argument(
        "--schema",
        choices=sorted(schemas_for()),
        help="export the active JSON Schema",
    )
    commands = result.add_subparsers(
        dest="command",
        title="commands",
        metavar=(
            "{init,runtime,config,device-check,catalogue,plan,verify,overhead,probe,run,resume,repeat-summary,"
            "prepare-length,rescore,export,public,report,compare,migrate,filter-candidates,"
            "extension,engine-fit}"
        ),
    )
    community.add_commands(commands)
    discovery.add_commands(commands)
    verification.add_commands(commands)
    overhead.add_commands(commands)
    execution.add_commands(commands)
    repetition.add_commands(commands)
    length.add_commands(commands)
    scoring.add_commands(commands)
    exports.add_commands(commands)
    public.add_commands(commands)
    reporting.add_commands(commands)
    comparison.add_commands(commands)
    migration.add_commands(commands)
    candidates.add_commands(commands)
    extensions.add_commands(commands)
    engine_fit.add_commands(commands)
    return result
