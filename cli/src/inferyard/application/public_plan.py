"""Command results for public candidate materialization and declared-config checks."""

from inferyard.application.types import CommandResult


def execute(request):
    if request.command == "public-config-check":
        from inferyard.config.public_config_check import check_config

        result = check_config(request.run, request.config)
        passed = result["declared_configuration_matches"]
        return (0 if passed else 3), CommandResult(
            request.command,
            "matched" if passed else "mismatch",
            "incomplete",
            details=result,
            limitations=tuple(result["limitations"]),
        )
    from inferyard.config.public_plan import materialize

    result = materialize(request.run, request.config, request.out)
    return 0, CommandResult(
        request.command,
        "planned",
        "incomplete",
        evidence_dir=str(request.out),
        details=result,
        limitations=tuple(result["limitations"]),
    )
