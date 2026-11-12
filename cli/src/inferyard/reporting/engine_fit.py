"""Sealed, independently verifiable engine-fit runs and portable comparisons."""

import hashlib
import re
from pathlib import Path
from stat import S_ISREG

from inferyard.config.engine_fit import validate_plan
from inferyard.contracts.validation import strict_json_loads, json_bytes
from inferyard.errors import ContractError, EvidenceError
from inferyard.evidence.storage import atomic_bytes
from inferyard.analysis.engine_fit_artifacts import (
    RUN_FILES,
    decode_json_bytes,
    sha256_hex,
    entry_path,
    manifest_document,
    load_manifest,
    manifest_present,
    read_entry,
    require_directory,
)
from inferyard.analysis.engine_fit_html import LIMITATIONS, render
from inferyard.analysis.engine_fit_metrics import observations
from inferyard.analysis.engine_fit_validation import fields, require, validate_run

def _verified_run(root, *, rerender=False, verified=None):
    root = require_directory(root)
    manifest, blobs = verified if verified is not None else load_manifest(root)
    require(manifest["kind"] == "run", "run_required")
    try:
        plan = validate_plan(decode_json_bytes(blobs["plan.json"]))
    except ContractError as exc:
        raise EvidenceError("engine_fit_invalid_plan_evidence") from exc
    run = decode_json_bytes(blobs["run.json"])
    rows = decode_json_bytes(blobs["requests.json"])
    validate_run(plan, run, rows)
    data = {"kind": "engine_fit_run", "plan": plan, "run": run, "requests": rows}
    if rerender:
        require(blobs["report.html"] == render([data]).encode("utf-8"), "html_rebuild_mismatch")
    return {**data, "manifest": manifest}, blobs


def _comparison(runs, source_hashes):
    require(len(runs) == 2, "comparison_run_count")
    plan = runs[0]["plan"]
    seen = set()
    for data in runs:
        require(data["plan"] == plan, "comparison_plan_mismatch")
        engine = data["run"]["engine"]
        require(engine not in seen, "duplicate_engine")
        seen.add(engine)
        require(
            data["run"]["service"]["measurement_source_sha256"]
            == runs[0]["run"]["service"]["measurement_source_sha256"],
            "comparison_measurement_source_mismatch",
        )
    return {
        "schema_version": 3,
        "definition": "engine_fit_comparison.v1",
        "diagnostic": True,
        "performance_comparison_qualified": False,
        "plan_id": plan["plan_id"],
        "model": plan["model"],
        "host": plan["host"],
        "source_runs": [
            {"path": f"sources/{index}", "manifest_sha256": digest}
            for index, digest in enumerate(source_hashes)
        ],
        "sides": [
            {
                "source_index": index,
                "run": data["run"],
                "requests": data["requests"],
                "observations": observations(data),
            }
            for index, data in enumerate(runs)
        ],
        "limitations": LIMITATIONS,
    }


def compare(paths, out):
    """Copy verified bytes to a new bundle; source paths never become read authority."""
    paths, out = list(paths), Path(out)
    require(len(paths) == 2, "comparison_run_count")
    require(not out.exists() and not out.is_symlink(), "output_exists")
    require(
        all(not out.resolve().is_relative_to(Path(path).resolve()) for path in paths),
        "output_inside_source",
    )
    sources = [_verified_run(path) for path in paths]
    runs = [data for data, _ in sources]
    result = _comparison(runs, [sha256_hex(blobs["manifest.json"]) for _, blobs in sources])
    html = render(runs, comparison=True).encode("utf-8")
    try:
        out.mkdir(parents=True, mode=0o700)
        (out / "sources").mkdir(mode=0o700)
        for index, (_, blobs) in enumerate(sources):
            destination = out / "sources" / str(index)
            destination.mkdir(mode=0o700)
            for name, content in blobs.items():
                atomic_bytes(destination / name, content)
    except OSError as exc:
        raise EvidenceError("engine_fit_output_creation_failed") from exc
    blobs = {"comparison.json": json_bytes(result), "report.html": html}
    for name, content in blobs.items():
        atomic_bytes(out / name, content)
    atomic_bytes(out / "manifest.json", json_bytes(manifest_document(blobs, "comparison")))
    return result


def verify(path, *, rerender=False):
    """Verify hashes, plan/row semantics, copied source seals and deterministic HTML."""
    root = require_directory(path)
    if not manifest_present(root):
        from inferyard.reporting.engine_fit_checkpoint import read_partial

        return read_partial(root)
    manifest, blobs = load_manifest(root)
    # A final manifest is authoritative; the retained in-flight checkpoint is ignored.
    if manifest["kind"] == "run":
        return _verified_run(root, rerender=rerender, verified=(manifest, blobs))[0]
    saved = decode_json_bytes(blobs["comparison.json"])
    require(type(saved) is dict, "comparison_type")
    refs = saved.get("source_runs")
    require(type(refs) is list and len(refs) == 2, "comparison_sources")
    source_root = require_directory(root / "sources")
    runs, hashes = [], []
    for index, ref in enumerate(refs):
        fields(ref, {"path", "manifest_sha256"}, "source_reference")
        require(ref["path"] == f"sources/{index}", "unsafe_source_reference")
        data, source_blobs = _verified_run(source_root / str(index), rerender=rerender)
        digest = sha256_hex(source_blobs["manifest.json"])
        require(ref["manifest_sha256"] == digest, "source_hash_mismatch")
        runs.append(data)
        hashes.append(digest)
    rebuilt = _comparison(runs, hashes)
    # Canonical bytes distinguish bool from int (Python structural equality does not).
    require(json_bytes(saved) == json_bytes(rebuilt), "comparison_rebuild_mismatch")
    if rerender:
        require(
            blobs["report.html"] == render(runs, comparison=True).encode(), "html_rebuild_mismatch"
        )
    return {"kind": "engine_fit_comparison", "comparison": rebuilt, "manifest": manifest}
