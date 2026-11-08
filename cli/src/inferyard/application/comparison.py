"""Lazy comparison command dispatch; no live backend loads on help or schema."""

from inferyard.application.types import CommandRequest, CommandResult


def execute(request: CommandRequest) -> tuple[int, CommandResult]:
    if request.command == "compare-plan":
        from inferyard.config.comparison_plan import execute as run

        return run(request)
    from inferyard.reporting.comparison_report import execute as run

    return run(request)
