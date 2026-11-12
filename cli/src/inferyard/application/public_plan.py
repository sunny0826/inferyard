"""Command results for public candidate materialization and declared-config checks."""

from inferyard.application.types import CommandResult


def execute(request):
    from inferyard.reporting.public_package import verify_public

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
    from inferyard.analysis.scoring import scorer_hash
    from inferyard.config.public_plan import (
        build_reproduction,
        read_candidate,
        require_declared_config,
    )
    from inferyard.errors import EvidenceError

    verify_public(request.run)
    require_declared_config(request.run, request.config)
    candidate = read_candidate(request.run)
    if candidate["identities"]["scorer_sha256"] != scorer_hash():
        raise EvidenceError("public_plan_scorer_source_mismatch")
    result = build_reproduction(request.run, request.config, candidate, request.out)
    return 0, CommandResult(
        request.command,
        "planned",
        "incomplete",
        evidence_dir=str(request.out),
        details=result,
        limitations=tuple(result["limitations"]),
    )
