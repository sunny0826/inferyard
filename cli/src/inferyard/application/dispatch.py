"""Explicit command dispatch with backends imported only for actual execution."""

import os
from dataclasses import replace

from inferyard.application.types import CommandRequest, CommandResult
from inferyard.errors import ContractError, PreflightError


def default_backend(request: CommandRequest) -> tuple[int, CommandResult]:
    if request.command == "speed-results" or (request.command == "plan" and request.preset):
        from inferyard.application.context_speed import execute

        return execute(request)
    if request.command == "service discover":
        from inferyard.application.service_discovery import execute

        return execute(request)
    if request.command == "progress":
        from inferyard.application.progress import execute

        return execute(request)
    if request.command == "memory-check":
        from inferyard.application.memory_check import execute

        return execute(request)
    if request.command == "model acquire":
        from inferyard.application.model_source import execute

        return execute(request)
    if request.command == "engine acquire":
        from inferyard.application.engine_source import execute

        return execute(request)
    if request.command == "engine check":
        from inferyard.application.engine_check import execute

        return execute(request)
    if request.command in (
        "init",
        "runtime prepare",
        "config assets",
        "config create",
        "config bind",
        "config describe",
    ):
        from inferyard.application.community import execute

        return execute(request)
    if request.command in (
        "engine-fit engines",
        "engine-fit plan",
        "engine-fit run",
        "engine-fit compare",
        "engine-fit verify",
    ):
        from inferyard.application.engine_fit import execute

        return execute(request)
    if request.command == "verify":
        from inferyard.application.verification import execute

        return execute(request)
    aliases = {
        "probe": "check",
        "public package": "public-package",
        "public plan": "public-plan",
        "extension freeze": "extension-freeze",
        "extension run": "extension-run",
        "extension replay": "extension-replay",
    }
    if request.command in aliases:
        code, result = default_backend(replace(request, command=aliases[request.command]))
        return code, replace(result, command=request.command)
    if request.command == "device-check":
        from inferyard.application.device import execute

        return execute(request)
    if request.command in (
        "extension-freeze",
        "extension-run",
        "extension-replay",
        "extension-check",
    ):
        from inferyard.application.extensions import execute

        return execute(request)
    if request.command == "public-plan":
        from inferyard.application.public_plan import execute

        return execute(request)
    if request.command == "public-config-check":
        from inferyard.application.public_plan import execute

        return execute(request)
    if request.command in ("public-package", "public-check"):
        from inferyard.application.reporting import execute

        return execute(request)
    if request.command == "prepare-length":
        if os.name == "nt":
            raise PreflightError("windows_phase2_live_not_supported")
        from inferyard.application.execution import execute

        return execute(request)
    if request.command in ("rescore", "rescore-check"):
        from inferyard.application.reporting import execute

        return execute(request)
    if request.command in ("export", "export-check"):
        from inferyard.application.reporting import execute

        return execute(request)
    if request.command in ("report", "report-check"):
        from inferyard.application.reporting import execute

        return execute(request)
    if request.command == "filter-candidates":
        from inferyard.application.candidates import execute

        return execute(request)
    if request.command == "compare-engines":
        from inferyard.application.reporting import execute

        return execute(request)
    if request.command in ("compare-plan", "compare", "compare-check"):
        from inferyard.application.comparison import execute

        return execute(request)
    if request.command in ("repeat-summary", "repeat-check"):
        from inferyard.application.reporting import execute

        return execute(request)
    if request.command in ("overhead", "overhead-check"):
        from inferyard.application.overhead import execute

        return execute(request)
    if request.command == "resume" or (
        request.command == "run" and request.frozen_plan is not None
    ):
        from inferyard.application.execution import execute

        return execute(request)
    if request.command == "catalogue":
        from inferyard.registry import catalogue

        return 0, CommandResult(
            "catalogue", "listed", "complete", details=catalogue(request.catalogue_kind)
        )
    if request.command == "plan":
        from inferyard.config.planning import prepare_plan, write_plan

        plan = (
            prepare_plan(request.experiment)[0]
            if request.dry_run
            else write_plan(request.experiment, request.out)
        )
        return 0, CommandResult(
            "plan",
            "previewed" if request.dry_run else "frozen",
            "complete",
            evidence_dir=None if request.dry_run else str(request.out),
            limitations=tuple(plan["limitations"]),
            details=plan,
        )
    if request.command in ("check", "run"):
        from inferyard.application.execution import execute

        return execute(request)
    raise ContractError("command", "unsupported command")
