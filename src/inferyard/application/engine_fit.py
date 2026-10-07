"""Lazy application dispatch for independent engine adaptation diagnostics."""

from inferyard.application.types import CommandResult
from inferyard.contracts.validation import ContractError
from inferyard.platforms.identity import PreflightError


def execute(request):
    command = request.command
    if command == "engine-fit engines":
        from inferyard.config.engine_fit_capabilities import capabilities

        return 0, CommandResult(command, "listed", "complete", details=capabilities())
    if command == "engine-fit plan":
        from inferyard.config.engine_fit import prepare

        data = prepare(request)
        return 0, CommandResult(
            command, "frozen", "complete", evidence_dir=str(request.out), details=data
        )
    if command == "engine-fit run":
        from inferyard.runtime.engine_fit import execute as run

        try:
            return run(request)
        except PreflightError as exc:
            # PreflightError contains fixed categories only, never OS/service response text.
            return 2, CommandResult(command, "blocked", limitations=(str(exc),))
    if command == "engine-fit compare":
        from inferyard.reporting.engine_fit import compare

        data = compare(request.report_runs, request.out)
        return 0, CommandResult(
            command, "compared", "complete", evidence_dir=str(request.out), details=data
        )
    if command == "engine-fit verify":
        from inferyard.reporting.engine_fit import verify

        data = verify(request.run, rerender=request.rerender)
        sealed = "manifest" in data
        execution = data.get("run")
        return 0 if sealed else 3, CommandResult(
            command,
            "verified" if sealed else "partial",
            execution["completeness"] if execution else "complete",
            evidence_dir=str(request.run),
            details={
                "kind": data["kind"],
                "integrity": "sealed" if sealed else "unsealed",
                "sealed": sealed,
                "verified": sealed,
                "semantic_validation": True,
                "bytes_verified": sealed,
                "sources_verified": sealed,
                "semantic_verified": True,
                "render_checked": sealed
                and (
                    request.rerender or data["manifest"]["definition"] == "engine_fit_manifest.v1"
                ),
                **(
                    {
                        "execution_completeness": execution["completeness"],
                        "stop_reason": execution["stop_reason"],
                    }
                    if execution
                    else {}
                ),
                **({"requests": data["requests"], "run": data["run"]} if "run" in data else {}),
            },
        )
    raise ContractError("command", "unsupported command")
