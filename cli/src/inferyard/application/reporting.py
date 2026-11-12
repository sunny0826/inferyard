"""Command results for offline reporting, rescoring, export and comparison."""

from inferyard.application.types import CommandResult


def _verify_reports(request, options):
    from inferyard.reporting.report import verify_report

    return 0, CommandResult(
        request.command,
        "verified",
        "complete",
        details=verify_report(request.run, options=options),
    )


def _report(request):
    from inferyard.reporting.report import write_report

    roots = request.report_runs or [request.run]
    index = write_report(roots, request.out, comparison_path=request.comparison_path)
    return 0, CommandResult(
        request.command,
        "rendered",
        "complete",
        evidence_dir=str(request.out),
        limitations=tuple(index["limitations"]),
        details={"runs": len(index["runs"]), "report": str(request.out / "report.html")},
    )


def _rescore_check(request):
    from inferyard.reporting.rescore import verify_rescore

    return 0, CommandResult(
        request.command, "verified", "complete", details=verify_rescore(request.run)
    )


def _rescore(request):
    from inferyard.reporting.rescore import write_rescore

    analysis = write_rescore(
        request.run,
        request.out,
        scorer_id=request.scorer_id,
        reason=request.revision_reason,
        parent_path=request.analysis_path,
    )
    return 0, CommandResult(
        request.command,
        "rescored",
        "complete",
        evidence_dir=str(request.out),
        limitations=tuple(analysis["limitations"]),
        details={
            "analysis_id": analysis["analysis_id"],
            "parent_analysis_id": analysis["parent_analysis_id"],
        },
    )


def _export_check(request):
    from inferyard.reporting.export import verify_export

    return 0, CommandResult(
        request.command, "verified", "complete", details=verify_export(request.run)
    )


def _export(request):
    from inferyard.reporting.export import write_export

    result = write_export(request.out, run=request.run, analysis_path=request.analysis_path)
    return 0, CommandResult(
        request.command,
        "exported",
        "complete",
        evidence_dir=str(request.out),
        limitations=tuple(result["limitations"]),
        details=result,
    )


def _repeat_check(request):
    from inferyard.reporting.repetition_report import verify_repetition_summary

    return 0, CommandResult(
        request.command, "verified", "complete", details=verify_repetition_summary(request.run)
    )


def _repeat(request):
    from inferyard.reporting.repetition_report import write_repetition_summary

    result = write_repetition_summary(request.run, request.out)
    return 0, CommandResult(
        request.command,
        "derived",
        "complete",
        evidence_dir=str(request.out),
        limitations=tuple(result["limitations"]),
        details=result,
    )


def _public(request):
    from inferyard.reporting.public_package import LIMITS, verify_public, write_public

    result = (
        verify_public(request.run, request.from_run)
        if request.command == "public-check"
        else write_public(request.run, request.out)
    )
    return 0, CommandResult(
        request.command,
        "verified" if request.command == "public-check" else "exported",
        "complete",
        details=result,
        limitations=tuple(LIMITS),
    )


def _compare_check(request, options):
    from inferyard.reporting.comparison_report import verify_comparison

    result = verify_comparison(request.run, source_roots=options.source_roots)
    return 0, CommandResult(request.command, "verified", "complete", details=result)


def _compare(request):
    from inferyard.reporting.comparison_report import write_comparison

    result = write_comparison(request)
    return 0, CommandResult(
        request.command,
        "compared",
        "complete",
        evidence_dir=str(request.out),
        limitations=tuple(result["limitations"]),
        details=result,
    )


def _engine_compare_check(request, options):
    from inferyard.reporting.engine_comparison_v2_report import verify

    return 0, CommandResult(
        request.command, "verified", "complete", details=verify(request.run, options=options)
    )


def _engine_comparison(request):
    from inferyard.reporting.engine_comparison_v2_report import write

    result = write(request)
    return 0, CommandResult(
        request.command,
        "derived",
        "complete",
        evidence_dir=str(request.out),
        limitations=tuple(result["limitations"]),
        details=result,
    )


def execute(request, *, options=None):
    if request.command in ("report-check", "compare-check", "engine-compare-check"):
        if options is None:
            from inferyard.application.verification import verification_options

            options = verification_options(request)
    if request.command == "report-check":
        return _verify_reports(request, options)
    if request.command == "report":
        return _report(request)
    if request.command == "rescore-check":
        return _rescore_check(request)
    if request.command == "rescore":
        return _rescore(request)
    if request.command == "export-check":
        return _export_check(request)
    if request.command == "export":
        return _export(request)
    if request.command == "repeat-check":
        return _repeat_check(request)
    if request.command == "repeat-summary":
        return _repeat(request)
    if request.command in ("public-check", "public-package"):
        return _public(request)
    if request.command == "compare-check":
        return _compare_check(request, options)
    if request.command == "compare":
        return _compare(request)
    if request.command == "engine-compare-check":
        return _engine_compare_check(request, options)
    if request.command == "compare-engines":
        return _engine_comparison(request)
    from inferyard.errors import ContractError

    raise ContractError("command", "unsupported command")
