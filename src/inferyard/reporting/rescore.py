"""Offline scoring revisions preserve execution history and original analyses."""

import hashlib
from copy import deepcopy
from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.analysis.observations import Observations, quality_observations
from inferyard.analysis.scoring import ScoringContext, score_case, scorer_hash, unscorable
from inferyard.analysis.scoring_revision import resolve
from inferyard.application.types import CommandResult
from inferyard.contracts.validation import ContractError, validate_document
from inferyard.evidence.storage import (
    EvidenceError,
    atomic_bytes,
    json_bytes,
    local_file,
    read_json,
)
from inferyard.provenance import tool_source_hash
from inferyard.reporting.comparison_report import comparison_input
from inferyard.reporting.export import trial_analysis
from inferyard.reporting.report_common import _new_output


def rescore_rows(
    cases,
    requests,
    policy,
    *,
    scorer=score_case,
    identity=scorer_hash,
    missing=unscorable,
    context=None,
    version="phase2.v1",
):
    context = context or ScoringContext()
    functions = resolve(version)
    builtin = (
        scorer is functions[0]
        and identity in (scorer_hash, functions[1])
        and missing in (unscorable, functions[2])
    )
    selected = {r["case_id"] for r in requests if r["execution_state"] == "completed"}
    if builtin:
        context.prepare([case for case in cases if case["case_id"] in selected])
        identity_value = context.identity(version)

        def missing(category, reason):
            return context.missing(category, reason, version)
    else:
        identity_value = context.identity() if identity is scorer_hash else identity()
        if missing is unscorable:
            missing = context.missing
    by_id = {case["case_id"]: case for case in cases}
    rows, changes = [], []
    for request in requests:
        row = deepcopy(request)
        old = row.get("score")
        row["score"] = None
        if row["execution_state"] == "completed":
            if (old or {}).get("reason") == "redacted_scoring_input" or "[REDACTED]" in row[
                "content"
            ]:
                new = missing(row["category"], "redacted_scoring_input")
            else:
                try:
                    if builtin:
                        new = context.score(row["case_id"], row["content"], policy, version)
                    else:
                        new = scorer(by_id[row["case_id"]], row["content"], policy)
                        validate_document("score", new)
                except EvidenceError, OSError:
                    raise
                except Exception:
                    new = missing(row["category"], "scorer_exception")
                if new["category"] != row["category"] or new["scorer_sha256"] != identity_value:
                    raise EvidenceError("rescore_scorer_identity_mismatch")
            row["score"] = new
        changes.append(
            {
                "request_id": row["request_id"],
                "case_id": row["case_id"],
                "execution_state": row["execution_state"],
                "old_score": old,
                "new_score": row["score"],
            }
        )
        rows.append(row)
    return rows, changes


def replay_lineage(root, files, depth, *, context=None, loaded=None):
    if depth > 16:
        raise EvidenceError("rescore_lineage_depth_exceeded")
    if (
        type(files) is not dict
        or not {"analysis.json", "rescore.json", "parent-analysis.json"} <= files.keys()
    ):
        raise EvidenceError("rescore_parent_evidence_missing")
    record = files["rescore.json"]
    _check_saved_hashes(files)
    expected = build_rescore(
        root,
        scorer_id=record["scorer_id"],
        reason=record["reason"],
        parent=files["parent-analysis.json"],
        producer=record["producer"],
        parent_evidence=files.get("parent-lineage.json"),
        depth=depth,
        context=context,
        loaded=loaded,
        saved_record=record,
    )
    if files != expected:
        raise EvidenceError("rescore_parent_recomputation_mismatch")
    return expected


def build_rescore(
    root,
    *,
    scorer_id,
    reason,
    parent=None,
    producer=None,
    parent_evidence=None,
    depth=0,
    context=None,
    loaded=None,
    saved_record=None,
):
    context = context or ScoringContext()
    scorer, identity, missing = resolve(scorer_id)
    if not reason or not reason.strip() or len(reason) > 500:
        raise EvidenceError("rescore_reason_required")
    producer = producer or tool_source_hash()
    loaded = loaded if loaded is not None else comparison_input(root)
    data, source = loaded
    if saved_record is not None and saved_record["source"] != source:
        raise EvidenceError("rescore_parent_source_mismatch")
    original, _ = trial_analysis(root, producer=producer, loaded=loaded)
    parent = deepcopy(parent) if parent is not None else original
    from inferyard.evidence.formats import require_core

    if parent_evidence is not None:
        _check_saved_hashes(parent_evidence)
    require_core(parent, "analysis")
    validate_document("analysis", parent)
    if parent["source_runs"] != original["source_runs"]:
        raise EvidenceError("rescore_parent_source_mismatch")
    requests = deepcopy(data["requests"])
    if parent_evidence is None:
        if parent != original:
            raise EvidenceError("rescore_parent_evidence_required")
    else:
        previous = replay_lineage(root, parent_evidence, depth + 1, context=context, loaded=loaded)
        if parent != previous["analysis.json"]:
            raise EvidenceError("rescore_parent_analysis_mismatch")
        scores = {r["request_id"]: r["new_score"] for r in previous["rescore.json"]["changes"]}
        for row in requests:
            row["score"] = scores[row["request_id"]]
    if saved_record is not None and saved_record["scorer_sha256"] != context.identity(scorer_id):
        raise EvidenceError("rescore_scorer_identity_changed")
    rows, changes = rescore_rows(
        data["bundle"]["cases"],
        requests,
        data["bundle"]["answer_policy"],
        scorer=scorer,
        identity=identity,
        missing=missing,
        context=context,
        version=scorer_id,
    )
    record = {
        "schema_version": SCHEMA_VERSION,
        "format_version": 1,
        "source": source,
        "producer": producer,
        "scorer_id": scorer_id,
        "scorer_sha256": context.identity(scorer_id),
        "reason": reason,
        "parent_analysis_id": parent["analysis_id"],
        "changes": changes,
    }
    if parent_evidence is not None:
        record.update(
            parent_lineage_sha256=hashlib.sha256(json_bytes(parent_evidence)).hexdigest(),
        )
    record_hash = hashlib.sha256(json_bytes(record)).hexdigest()
    aid = "rescore-" + record_hash[:32]
    record["analysis_id"] = aid
    evidence = [
        {"path": name, "sha256": hashlib.sha256(json_bytes(value)).hexdigest()}
        for name, value in (("rescore.json", record), ("parent-analysis.json", parent))
    ]
    if parent_evidence is not None:
        evidence.append({"path": "parent-lineage.json", "sha256": record["parent_lineage_sha256"]})
    run = deepcopy(data["run"])
    run["definition_versions"]["scoring"] = scorer_id
    trial = next(t for t in data["plan"]["trials"] if t["trial_id"] == run["trial_id"])
    repeated = "duration" in data["summary"]
    observations = Observations(
        run, trial["workload_id"], evidence, complete=data["summary"]["completeness"] == "complete"
    )
    if not repeated:
        by_id = {case["case_id"]: case for case in data["bundle"]["cases"]}
        for category in sorted({row["category"] for row in rows}):
            selected = [row for row in rows if row["category"] == category]
            quality_observations(
                observations, [by_id[row["case_id"]] for row in selected], selected, category
            )
    analysis = {
        **original,
        "analysis_id": aid,
        "parent_analysis_id": parent["analysis_id"],
        "reason": reason,
        "definition_versions": run["definition_versions"],
        "scorer_id": scorer_id,
        "scorer_sha256": context.identity(scorer_id),
        "metrics": observations.items,
        "limitations": [
            "quality_only_revision_execution_unchanged",
            "same_frozen_tasks_and_answer_policy",
            *(["duration_scores_are_repeated_probes_not_whole_bundle"] if repeated else []),
        ],
    }
    from inferyard.evidence.formats import require_core

    require_core(analysis, "analysis")
    validate_document("analysis", analysis)
    files = {"analysis.json": analysis, "rescore.json": record, "parent-analysis.json": parent}
    if parent_evidence is not None:
        files["parent-lineage.json"] = parent_evidence
    return files


def write_rescore(root, out, *, scorer_id, reason, parent_path=None):
    parent = read_json(parent_path) if parent_path else None
    lineage = None
    if parent_path and parent["parent_analysis_id"] is not None:
        lineage = read_revision(parent_path.parent)
    files = build_rescore(
        root, scorer_id=scorer_id, reason=reason, parent=parent, parent_evidence=lineage
    )
    _new_output(out, [root, *([parent_path.parent] if parent_path else [])])
    for name, value in files.items():
        atomic_bytes(out / name, json_bytes(value))
    return files["analysis.json"]


def read_revision(out):
    record = read_json(local_file(out, "rescore.json"))
    if record.get("schema_version") != SCHEMA_VERSION or record.get("format_version") != 1:
        raise EvidenceError("unsupported_rescore_format")
    names = ["analysis.json", "rescore.json", "parent-analysis.json"]
    if "parent_lineage_sha256" in record:
        names.append("parent-lineage.json")
    return {name: read_json(local_file(out, name)) for name in names}


def _check_saved_hashes(files):
    """Check stored lineage bindings before rejecting formats or scorer identities."""
    try:
        record, analysis = files["rescore.json"], files["analysis.json"]
        if any(
            type(value) is not dict for value in (record, analysis, files["parent-analysis.json"])
        ):
            raise EvidenceError("rescore_parent_evidence_missing")
        if type(analysis.get("metrics")) is not list:
            raise EvidenceError("rescore_parent_evidence_missing")
        body = {k: v for k, v in record.items() if k != "analysis_id"}
        aid = "rescore-" + hashlib.sha256(json_bytes(body)).hexdigest()[:32]
        if record["analysis_id"] != aid or analysis["analysis_id"] != aid:
            raise EvidenceError("rescore_recomputation_mismatch")
        if record["parent_analysis_id"] != files["parent-analysis.json"]["analysis_id"]:
            raise EvidenceError("rescore_parent_analysis_mismatch")
        hashes = {
            name: hashlib.sha256(json_bytes(value)).hexdigest() for name, value in files.items()
        }
        for metric in analysis["metrics"]:
            if type(metric) is not dict or type(metric.get("evidence_refs")) is not list:
                raise EvidenceError("rescore_parent_evidence_missing")
            for ref in metric["evidence_refs"]:
                if (
                    type(ref) is not dict
                    or type(ref.get("path")) is not str
                    or type(ref.get("sha256")) is not str
                    or len(ref["sha256"]) != 64
                    or any(c not in "0123456789abcdef" for c in ref["sha256"])
                ):
                    raise EvidenceError("rescore_parent_evidence_missing")
                if ref["path"] in files and hashes[ref["path"]] != ref["sha256"]:
                    raise EvidenceError("rescore_parent_recomputation_mismatch")
        if "parent_lineage_sha256" in record:
            lineage = files["parent-lineage.json"]
            if hashlib.sha256(json_bytes(lineage)).hexdigest() != record["parent_lineage_sha256"]:
                raise EvidenceError("rescore_parent_recomputation_mismatch")
        from inferyard.evidence.formats import require_core

        for document in (analysis, files["parent-analysis.json"]):
            require_core(document, "analysis")
            validate_document("analysis", document)
    except (KeyError, TypeError, ContractError) as exc:
        raise EvidenceError("rescore_parent_evidence_missing") from exc


def verify_rescore(out):
    files = read_revision(out)
    record = files["rescore.json"]
    _check_saved_hashes(files)
    parent = files["parent-analysis.json"]
    expected = build_rescore(
        Path(record["source"]["path"]),
        scorer_id=record["scorer_id"],
        reason=record["reason"],
        parent=parent,
        producer=record["producer"],
        parent_evidence=files.get("parent-lineage.json"),
        saved_record=record,
    )
    for name, value in expected.items():
        if local_file(out, name).read_bytes() != json_bytes(value):
            raise EvidenceError("rescore_recomputation_mismatch")
    return {
        "verified": True,
        "analysis_id": expected["analysis.json"]["analysis_id"],
        "requests": len(record["changes"]),
    }


def execute(request):
    if request.command == "rescore-check":
        return 0, CommandResult(
            request.command, "verified", "complete", details=verify_rescore(request.run)
        )
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
