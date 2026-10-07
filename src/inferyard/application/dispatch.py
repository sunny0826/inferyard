"""Explicit command dispatch with backends imported only for actual execution."""

import os
from dataclasses import replace

from inferyard.application.types import CommandRequest, CommandResult
from inferyard.contracts.validation import ContractError
from inferyard.platforms.identity import PreflightError


def default_backend(request: CommandRequest) -> tuple[int, CommandResult]:
    if request.command == "host-state migrate":
        from inferyard.runtime.host_migration import migrate

        result = migrate()
        return 0, CommandResult(request.command, result["status"], "complete", details=result)
    if request.command in (
        "init",
        "runtime prepare",
        "config assets",
        "config create",
        "config bind",
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
        from inferyard.platforms.device_preflight import execute

        return execute(request)
    if request.command in (
        "extension-freeze",
        "extension-run",
        "extension-replay",
        "extension-check",
    ):
        from inferyard.extensions.workflow import execute

        return execute(request)
    if request.command == "public-plan":
        from inferyard.config.public_plan import execute

        return execute(request)
    if request.command == "public-config-check":
        from inferyard.config.public_config_check import execute

        return execute(request)
    if request.command in ("public-package", "public-check"):
        from inferyard.reporting.public_package import execute

        return execute(request)
    if request.command == "prepare-length":
        if os.name == "nt":
            raise PreflightError("windows_phase2_live_not_supported")
        from inferyard.runtime.length_prepare import execute

        return execute(request)
    if request.command in ("rescore", "rescore-check"):
        from inferyard.reporting.rescore import execute

        return execute(request)
    if request.command in ("export", "export-check"):
        from inferyard.reporting.export import execute

        return execute(request)
    if request.command in ("report", "report-check"):
        from inferyard.reporting.report import execute

        return execute(request)
    if request.command == "filter-candidates":
        from inferyard.analysis.candidate_filter import execute

        return execute(request)
    if request.command in ("compare", "compare-check"):
        from inferyard.reporting.comparison_report import execute

        return execute(request)
    if request.command in ("repeat-summary", "repeat-check"):
        from inferyard.reporting.repetition_report import execute

        return execute(request)
    if request.command in ("overhead", "overhead-check"):
        from inferyard.application.overhead import execute

        return execute(request)
    if request.command == "resume" or (
        request.command == "run" and request.frozen_plan is not None
    ):
        from inferyard.runtime.batch_runner import execute

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
        from inferyard.runtime.runner import execute

        return execute(request)
    raise ContractError("command", "unsupported command")
