"""Rebuildable local run index and self-contained, escaped multi-run report."""

from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.analysis.comparison import compare_trials
from inferyard.application.types import CommandResult, VerificationOptions
from inferyard.evidence.formats import check_presentation_seal, require_core, require_version
from inferyard.evidence.storage import (
    EvidenceError,
    atomic_bytes,
    json_bytes,
    local_file,
    read_json,
    sha256_file,
)
from inferyard.provenance import tool_source_hash
from inferyard.reporting.comparison_report import comparison_input, read_verified_comparison
from inferyard.reporting.report_assets import template_hash
from inferyard.reporting.report_common import (
    _environment,
    _new_output,
    evidence_url,
    render_report_html,
)
from inferyard.reporting.report_dashboard import dashboard_view, request_details
from inferyard.reporting.report_duration import duration_view
from inferyard.reporting.report_facets import facet_options, run_facets
from inferyard.reporting.report_profile import profile_view
from inferyard.reporting.report_resources import resource_view
from inferyard.reporting.report_scans import scan_view


def presentation(data, source, out, *, format_version=7):
    summary = data["summary"]
    cases = {case["case_id"]: case for case in data["bundle"]["cases"]}
    rows = []
    for ordinal, request in enumerate(data["requests"], 1):
        start, end = request.get("t_send_ns"), request.get("t_terminal_ns")
        rows.append(
            {
                "case_id": request["case_id"],
                "prompt": cases[request["case_id"]]["prompt"],
                "state": request["execution_state"],
                "latency_ms": (end - start) / 1e6
                if start is not None and end is not None
                else None,
                "quality": (request.get("score") or {}).get("quality_state"),
                "content": request.get("content", ""),
                "error": request.get("error_category"),
            }
        )
        rows[-1].update(
            request_details(
                request, cases[request["case_id"]], ordinal, format_version=format_version
            )
        )
    values = [row["latency_ms"] for row in rows if row["latency_ms"] is not None]
    view = {
        "schema_version": SCHEMA_VERSION,
        "kind": data["run"]["kind"],
        "origin": data["run"]["origin"],
        "execution_mode": data["run"]["execution_mode"],
        "parent_run_id": data["run"]["parent_run_id"],
        "relation": data["run"]["relation"],
        "facets": run_facets(data, first_event_utc=data["first_event_utc"]),
        "source": source,
        "model": data["config"]["model"]["display_name"],
        "experiment_id": data["run"]["experiment_id"],
        "threads": data["config"]["conditions"]["threads"],
        "summary": summary,
        "duration_view": duration_view(summary),
        "resource_view": resource_view(data["samples"], data["config"]["telemetry"]["interval_ms"]),
        "scan_view": scan_view(summary, data["requests"]),
        "diagnostic": data["run"]["diagnostic"],
        "requests": rows,
        "latency_max": max(values, default=0) or 1,
        "evidence": {
            name: evidence_url(Path(source["path"]) / name, out)
            for name in ("manifest.json", "events.jsonl", "config.frozen.json", "plan.json")
        },
    }
    if summary.get("engine_observation") is not None:
        view["engine_observation"] = summary["engine_observation"]
    view["profile"] = profile_view(data)
    view["dashboard"] = dashboard_view(data, view["resource_view"])
    view["svg_gallery"] = [
        {
            "ordinal": row["ordinal"],
            "case_id": row["case_id"],
            "prompt": row["prompt"],
            "latency_ms": row["latency_ms"],
            "completion_tokens": row.get("completion_tokens"),
            "finish_reason": row.get("finish_reason"),
            "svg_view": row["svg_view"],
        }
        for row in rows
        if row.get("category") == "svg"
    ]
    return view


def build_index(
    roots,
    out,
    *,
    producer=None,
    comparison_path=None,
    format_version=7,
    loaded=None,
    verified_comparison=None,
):
    require_version({"version": format_version}, "version", (7,), "report")
    out = out.resolve()
    if comparison_path is not None:
        comparison_path = comparison_path.resolve()
    if not roots or len(roots) > 100:
        raise EvidenceError("report_requires_1_to_100_trials")
    loaded = (
        loaded
        if loaded is not None
        else [comparison_input(root, bind_unsealed=True) for root in roots]
    )
    ids = [ref["run_id"] for _, ref in loaded]
    if len(set(ids)) != len(ids):
        raise EvidenceError("duplicate_report_run")
    runs = [
        presentation(data, source, out, format_version=format_version) for data, source in loaded
    ]
    comparison = None
    comparison_source = None
    if comparison_path is not None:
        if len(loaded) != 2:
            raise EvidenceError("linked_comparison_requires_two_runs")
        comparison = (
            verified_comparison
            if verified_comparison is not None
            else read_verified_comparison(comparison_path, loaded=loaded)
        )
        path = local_file(comparison_path, "comparison.json")
        refs = comparison["source_runs"]
        if comparison.get("format_version") == 4:
            refs = [{**ref, "path": loaded[i][1]["path"]} for i, ref in enumerate(refs)]
        if refs != [ref for _, ref in loaded]:
            raise EvidenceError("report_comparison_sources_or_order_mismatch")
        comparison_source = {
            "path": str(comparison_path.resolve()),
            "sha256": sha256_file(path),
            "url": evidence_url(path, out),
        }
    elif len(loaded) == 2:
        comparison = compare_trials(
            loaded[0][0],
            loaded[1][0],
            definition="phase2.v3",
        )
    index = {
        "schema_version": SCHEMA_VERSION,
        "generator_source_sha256": producer or tool_source_hash(),
        "report_format_version": format_version,
        "filter_options": facet_options(runs),
        "template_sha256": template_hash(format_version),
        "runs": runs,
        "comparison": comparison,
        "limitations": [
            "runs_are_not_pooled",
            "display_filter_does_not_recompute_denominators",
            "local_private_report_not_public_export",
        ],
    }
    if comparison_source:
        index["comparison_source"] = comparison_source
    return index


def write_report(roots, out, *, comparison_path=None):
    from inferyard.contracts.validation import strict_json_loads
    from inferyard.reporting.sealed_report import portable_index, seal_report

    index = build_index(roots, out, comparison_path=comparison_path, format_version=7)
    index = portable_index(index, out, comparison_path)
    index = strict_json_loads(json_bytes(index).decode())
    html = render_report_html(_environment(), "report.html", index)
    protected = list(roots)
    if comparison_path is not None:
        protected.append(comparison_path)
        protected.extend(
            out / Path(proof["source"]["path"])
            for proof in index["comparison"].get("performance_evidence", [])
        )
    _new_output(out, protected)
    atomic_bytes(out / "index.json", json_bytes(index))
    atomic_bytes(out / "report.html", html.encode())
    seal_report(out)
    return index


def verify_report(out, *, options=None):
    saved = read_json(local_file(out, "index.json"))
    if (
        type(saved) is not dict
        or type(saved.get("schema_version")) is not int
        or type(saved.get("report_format_version")) is not int
        or saved.get("schema_version") != 3
        or saved.get("report_format_version") != 7
    ):
        check_presentation_seal(out)
    require_core(saved, "report")
    require_version(saved, "report_format_version", (7,), "report")
    from inferyard.reporting.sealed_report import verify

    return verify(out, saved, options=options or VerificationOptions())


def execute(request, *, options=None):
    if request.command == "report-check":
        return 0, CommandResult(
            request.command,
            "verified",
            "complete",
            details=verify_report(
                request.run, options=options or VerificationOptions.from_request(request)
            ),
        )
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
