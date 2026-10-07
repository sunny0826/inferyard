"""Sealed, independently verifiable engine-fit runs and portable comparisons."""

import hashlib
import re
from pathlib import Path
from stat import S_ISREG

from inferyard.config.engine_fit import validate_plan
from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.evidence.storage import EvidenceError, atomic_bytes, json_bytes
from inferyard.reporting.engine_fit_html import LIMITATIONS, render
from inferyard.reporting.engine_fit_metrics import observations
from inferyard.reporting.engine_fit_validation import fields, require, validate_run

RUN_FILES = ("plan.json", "run.json", "requests.json", "report.html")
COMPARISON_FILES = ("comparison.json", "report.html")


def _sha(content):
    return hashlib.sha256(content).hexdigest()


def _directory(path):
    path = Path(path)
    require(not path.is_symlink() and path.is_dir(), "unsafe_directory")
    return path


def _file(root, name):
    path = root / name
    require(not path.is_symlink(), "unsafe_file")
    require(not path.exists() or path.is_file(), "unsafe_file")
    return path


def _read(root, name):
    try:
        return _file(root, name).read_bytes()
    except OSError as exc:
        raise EvidenceError("engine_fit_missing_file") from exc


def _manifest_present(root):
    try:
        entry = (root / "manifest.json").lstat()
    except FileNotFoundError:
        return False
    except OSError as exc:
        raise EvidenceError("engine_fit_missing_file") from exc
    require(S_ISREG(entry.st_mode), "unsafe_file")
    return True


def _json(content):
    try:
        result = strict_json_loads(content.decode("utf-8"))
        pending = [result]
        while pending:
            item = pending.pop()
            if isinstance(item, str):
                item.encode("utf-8", "strict")
            elif isinstance(item, dict):
                pending.extend(item.keys())
                pending.extend(item.values())
            elif isinstance(item, list):
                pending.extend(item)
        return result
    except (UnicodeError, ContractError) as exc:
        raise EvidenceError("engine_fit_invalid_json") from exc


def _manifest(blobs, kind, *, definition="engine_fit_manifest.v2"):
    return {
        "schema_version": 3,
        "definition": definition,
        "kind": kind,
        "files": {name: _sha(content) for name, content in blobs.items()},
    }


def _load_manifest(root):
    content = _read(root, "manifest.json")
    manifest = _json(content)
    fields(manifest, {"schema_version", "definition", "kind", "files"}, "manifest_fields")
    require(manifest["kind"] in ("run", "comparison"), "manifest_kind")
    names = RUN_FILES if manifest["kind"] == "run" else COMPARISON_FILES
    fields(manifest["files"], set(names), "manifest_files")
    for digest in manifest["files"].values():
        require(type(digest) is str and re.fullmatch(r"[a-f0-9]{64}", digest), "manifest_hash")
    blobs = {name: _read(root, name) for name in names}
    require(
        manifest["files"] == {name: _sha(raw) for name, raw in blobs.items()},
        "hash_mismatch",
    )
    from inferyard.evidence.formats import require_core, require_version

    require_core(manifest, "engine-fit manifest")
    require_version(manifest, "definition", ("engine_fit_manifest.v2",), "engine-fit manifest")
    return manifest, {**blobs, "manifest.json": content}


def _verified_run(root, *, rerender=False, verified=None):
    root = _directory(root)
    manifest, blobs = verified if verified is not None else _load_manifest(root)
    require(manifest["kind"] == "run", "run_required")
    try:
        plan = validate_plan(_json(blobs["plan.json"]))
    except ContractError as exc:
        raise EvidenceError("engine_fit_invalid_plan_evidence") from exc
    run = _json(blobs["run.json"])
    rows = _json(blobs["requests.json"])
    validate_run(plan, run, rows)
    data = {"kind": "engine_fit_run", "plan": plan, "run": run, "requests": rows}
    if rerender:
        require(blobs["report.html"] == render([data]).encode("utf-8"), "html_rebuild_mismatch")
    return {**data, "manifest": manifest}, blobs


def seal_run(out, plan, run, rows):
    """Publish final snapshots and manifest last; an existing seal is never replaced."""
    root = _directory(out)
    require(not (root / "manifest.json").exists(), "already_sealed")
    _file(root, "manifest.json")
    validate_plan(plan)
    validate_run(plan, run, rows)
    data = {"plan": plan, "run": run, "requests": rows}
    blobs = {
        "plan.json": json_bytes(plan),
        "run.json": json_bytes(run),
        "requests.json": json_bytes(rows),
        "report.html": render([data]).encode("utf-8"),
    }
    # The runtime atomically checkpoints these three JSON files before final sealing.
    for name in RUN_FILES:
        _file(root, name)
    require(not (root / "report.html").exists(), "report_already_exists")
    for name, content in blobs.items():
        atomic_bytes(root / name, content, overwrite=name != "report.html")
    atomic_bytes(root / "manifest.json", json_bytes(_manifest(blobs, "run")))
    return run


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
    result = _comparison(runs, [_sha(blobs["manifest.json"]) for _, blobs in sources])
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
    atomic_bytes(out / "manifest.json", json_bytes(_manifest(blobs, "comparison")))
    return result


def verify(path, *, rerender=False):
    """Verify hashes, plan/row semantics, copied source seals and deterministic HTML."""
    root = _directory(path)
    if not _manifest_present(root):
        from inferyard.evidence.engine_fit_checkpoint import read_partial

        return read_partial(root)
    manifest, blobs = _load_manifest(root)
    # A final manifest is authoritative; the retained in-flight checkpoint is ignored.
    if manifest["kind"] == "run":
        return _verified_run(root, rerender=rerender, verified=(manifest, blobs))[0]
    saved = _json(blobs["comparison.json"])
    require(type(saved) is dict, "comparison_type")
    refs = saved.get("source_runs")
    require(type(refs) is list and len(refs) == 2, "comparison_sources")
    source_root = _directory(root / "sources")
    runs, hashes = [], []
    for index, ref in enumerate(refs):
        fields(ref, {"path", "manifest_sha256"}, "source_reference")
        require(ref["path"] == f"sources/{index}", "unsafe_source_reference")
        data, source_blobs = _verified_run(source_root / str(index), rerender=rerender)
        digest = _sha(source_blobs["manifest.json"])
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
