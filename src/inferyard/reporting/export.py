"""Consistent local analysis exports; source evidence is never overwritten."""

import csv
import hashlib
import html
import io
import json
import re
from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.application.types import CommandResult
from inferyard.contracts.validation import validate_document
from inferyard.evidence.storage import (
    EvidenceError,
    atomic_bytes,
    json_bytes,
    local_file,
    read_json,
    sha256_file,
)
from inferyard.provenance import tool_source_hash
from inferyard.reporting.comparison_report import comparison_input
from inferyard.reporting.report_common import _new_output


def trial_analysis(root, *, producer=None, loaded=None):
    data, source = loaded if loaded is not None else comparison_input(root)
    if source["manifest_sha256"] is None:
        raise EvidenceError("analysis_requires_sealed_source")
    producer = producer or tool_source_hash()
    identity = hashlib.sha256(json_bytes({"source": source, "producer": producer})).hexdigest()
    analysis = {
        "schema_version": SCHEMA_VERSION,
        "analysis_id": "trial-" + identity[:32],
        "source_runs": [{"run_id": source["run_id"], "manifest_sha256": source["manifest_sha256"]}],
        "parent_analysis_id": None,
        "reason": "offline_original_scoring_reduction",
        "definition_versions": data["run"]["definition_versions"],
        "scorer_id": data["run"]["definition_versions"]["scoring"],
        "scorer_sha256": data["selection"]["scorer_sha256"],
        "answer_policy_sha256": hashlib.sha256(
            json_bytes(data["bundle"]["answer_policy"])
        ).hexdigest(),
        "metrics": data["summary"]["metric_observations"],
        "limitations": [
            "local_export_not_public_redacted_package",
            "original_scoring_not_rescored",
        ],
    }
    validate_document("analysis", analysis)
    return analysis, {"kind": "trial", "path": str(root.resolve()), "producer": producer}


def existing_analysis(path):
    analysis = read_json(path)
    validate_document("analysis", analysis)
    for metric in analysis["metrics"]:
        for ref in metric["evidence_refs"]:
            if sha256_file(local_file(path.parent, ref["path"])) != ref["sha256"]:
                raise EvidenceError("analysis_evidence_hash_mismatch")
    return analysis, {"kind": "analysis", "path": str(path.resolve()), "sha256": sha256_file(path)}


def flat_rows(analysis):
    return [
        {
            "export_analysis_id": analysis["analysis_id"],
            **{k: v for k, v in metric.items() if k != "group"},
            **{"group_" + k: v for k, v in metric["group"].items()},
        }
        for metric in analysis["metrics"]
    ]


def scalar(value):
    if value is None:
        return ""
    if isinstance(value, (dict, list, bool)):
        return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return str(value)


def csv_cell(value):
    text = scalar(value)
    # Numeric negatives remain numeric. Untrusted text is explicitly textual.
    if isinstance(value, str) and (
        text.lstrip().startswith(("=", "+", "-", "@")) or text.startswith(("\t", "\r", "\n"))
    ):
        return "'" + text
    return text


def markdown_cell(value):
    text = html.escape(scalar(value), quote=True)
    text = re.sub(r"([\\`*_|\[\]()!])", r"\\\1", text)
    return text.replace("\r", "&#13;").replace("\n", "<br>")


def formatted_files(analysis):
    rows = flat_rows(analysis)
    columns = sorted({key for row in rows for key in row})
    stream = io.StringIO(newline="")
    writer = csv.writer(stream, lineterminator="\n")
    writer.writerow(columns)
    writer.writerows([csv_cell(row.get(key)) for key in columns] for row in rows)
    md = [
        "# Analysis " + markdown_cell(analysis["analysis_id"]),
        "",
        "Missing values are empty; status and missing_reason retain their meaning.",
        "CSV formula-like text is prefixed with an apostrophe. Numeric values are unchanged.",
        "",
        "| " + " | ".join(columns) + " |",
        "| " + " | ".join("---" for _ in columns) + " |",
    ]
    md.extend(
        "| " + " | ".join(markdown_cell(row.get(key)) for key in columns) + " |" for row in rows
    )
    return {
        "analysis.json": json_bytes(analysis),
        "metrics.csv": stream.getvalue().encode(),
        "metrics.md": ("\n".join(md) + "\n").encode(),
    }


def write_export(out, *, run=None, analysis_path=None):
    analysis, source = trial_analysis(run) if run is not None else existing_analysis(analysis_path)
    files = formatted_files(analysis)
    _new_output(out, [run if run is not None else analysis_path.parent])
    for name, content in files.items():
        atomic_bytes(out / name, content)
    manifest = {
        "format_version": 1,
        "analysis_id": analysis["analysis_id"],
        "source": source,
        "files": {name: hashlib.sha256(content).hexdigest() for name, content in files.items()},
        "limitations": [
            "local_private_export",
            "no_source_authentication_or_public_redaction",
            "evidence_references_resolve_against_original_source",
        ],
    }
    atomic_bytes(out / "export.json", json_bytes(manifest))
    return manifest


def verify_export(out):
    saved = read_json(local_file(out, "export.json"))
    source = saved["source"]
    if saved["format_version"] != 1 or source["kind"] not in ("trial", "analysis"):
        raise EvidenceError("unsupported_export_format")
    analysis, actual = (
        trial_analysis(Path(source["path"]), producer=source["producer"])
        if source["kind"] == "trial"
        else existing_analysis(Path(source["path"]))
    )
    files = formatted_files(analysis)
    hashes = {name: hashlib.sha256(content).hexdigest() for name, content in files.items()}
    if (
        actual != source
        or saved["analysis_id"] != analysis["analysis_id"]
        or saved["files"] != hashes
    ):
        raise EvidenceError("export_source_or_inventory_mismatch")
    for name, content in files.items():
        if local_file(out, name).read_bytes() != content:
            raise EvidenceError("export_recomputation_mismatch")
    return {
        "verified": True,
        "analysis_id": analysis["analysis_id"],
        "metric_rows": len(analysis["metrics"]),
    }


def execute(request):
    if request.command == "export-check":
        return 0, CommandResult(
            request.command, "verified", "complete", details=verify_export(request.run)
        )
    result = write_export(request.out, run=request.run, analysis_path=request.analysis_path)
    return 0, CommandResult(
        request.command,
        "exported",
        "complete",
        evidence_dir=str(request.out),
        limitations=tuple(result["limitations"]),
        details=result,
    )
