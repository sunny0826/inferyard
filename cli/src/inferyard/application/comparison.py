"""Lazy comparison command dispatch; no live backend loads on help or schema."""

from inferyard.application.types import CommandResult


def _compare_plan(request):
    from inferyard.config.comparison_plan import freeze

    plan = freeze(
        request.left_configs or (),
        request.right_configs or (),
        request.bundles or (),
        experiment_id=request.comparison_experiment_id,
        destination=None if request.dry_run else request.out,
    )
    return 0, CommandResult(
        "compare-plan",
        "previewed" if request.dry_run else "frozen",
        "complete",
        evidence_dir=None if request.dry_run else str(request.out),
        limitations=tuple(plan["limitations"]),
        details=plan,
    )


def execute(request):
    if request.command == "compare-plan":
        return _compare_plan(request)
    from inferyard.application.reporting import execute as run

    return run(request)
