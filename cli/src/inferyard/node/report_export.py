"""Studio v3 report export with explicit raw-verification provenance."""

from __future__ import annotations

import json
import uuid
from pathlib import Path

from inferyard.application.types import CommandRequest
from inferyard.contracts.validation import json_bytes, validate_document
from inferyard.evidence.storage import atomic_bytes, new_output


def list_reports(reports_root: Path) -> list[dict]:
    from inferyard.reporting.studio_report import read_studio_report

    if not reports_root.exists():
        return []
    result = []
    for marker in sorted(reports_root.glob("*/studio.json")):
        document = read_studio_report(marker.parent)
        result.append(
            {
                "path": str(marker),
                "run_ids": [run["source"]["run_id"] for run in document["runs"]],
                "runs": len(document["runs"]),
            }
        )
    return result


def _read_existing(source: Path) -> tuple[dict, Path]:
    from inferyard.reporting.studio_report import read_studio_report

    report_dir = source if source.is_dir() else source.parent
    return read_studio_report(report_dir), report_dir / "studio.json"


def _write_existing(source: Path, out: Path) -> dict:
    document, _ = _read_existing(source)
    new_output(out, [source.resolve()])
    atomic_bytes(out / "studio.json", json_bytes(document))
    return document


def _write_raw(sources: list[Path], out: Path) -> dict:
    from inferyard.application.raw_verification import execute
    from inferyard.reporting.studio_report import write_studio_report

    for source in sources:
        code, result = execute(CommandRequest("verify", run=source), "run")
        if code != 0 or not (result.details or {}).get("verified"):
            raise ValueError("raw evidence verification failed")
    return write_studio_report(sources, out)


def _classify_run_paths(paths: list[str]) -> tuple[list[str], list[str]]:
    from inferyard.analysis.ledger import read_trial
    from inferyard.evidence.storage import read_json
    from inferyard.provenance import tool_source_hash
    from inferyard.runtime import batch_runner

    resumable: list[str] = []
    rerunnable: list[str] = []
    source_identity = tool_source_hash()
    for raw in paths:
        run_dir = Path(raw)
        if not run_dir.is_dir():
            continue
        if (run_dir / "manifest.json").is_file() and (run_dir / "inputs.json").is_file():
            try:
                trial = read_trial(run_dir)
                meta = trial.get("run") or {}
                exp = ((trial.get("config") or {}).get("experiment")) or {}
                if (
                    meta.get("execution_mode") == "single"
                    and meta.get("kind") == "run"
                    and exp.get("profile") != "context-speed.v1"
                ):
                    rerunnable.append(raw)
            except Exception:
                pass
        if run_dir.parent.name == "runs" and (run_dir.parent.parent / "batch.json").is_file():
            try:
                root = run_dir.parent.parent.resolve()
                diagnostic = bool(read_json(root / "batch.json").get("diagnostic"))
                plan, loaded = batch_runner.open_batch(
                    root, None, diagnostic, source_identity=source_identity
                )
                runs, comparison_stop = batch_runner.load_plan_history(
                    root,
                    plan,
                    loaded,
                    options=batch_runner.HistoryLoadOptions(source_identity, None),
                )
                pending, interrupted = batch_runner._next(plan, runs)
                if (
                    comparison_stop is None
                    and not batch_runner._capacity_scan_stopped(plan, runs)
                    and pending is not None
                    and interrupted is not None
                    and "duration" not in interrupted["summary"]
                    and interrupted["path"].resolve() == run_dir.resolve()
                ):
                    resumable.append(raw)
            except Exception:
                pass
    return resumable, rerunnable


def export_report(
    *,
    state_dir: Path,
    run_path: str | None = None,
    run_paths: list[str] | None = None,
    report_path: str | None = None,
    out: str | None = None,
) -> dict:
    raw_inputs = [Path(p).resolve() for p in (run_paths or ([run_path] if run_path else []))]
    if bool(raw_inputs) == bool(report_path):
        raise ValueError("exactly one run_path/run_paths or report_path is required")
    verified_from_raw = bool(raw_inputs)
    if not verified_from_raw and out is None:
        source = Path(report_path).resolve()
        document, report_file = _read_existing(source)
        validate_document("studio_report", document)
        source_paths = [
            path
            for run in document.get("runs", [])
            if isinstance((path := (run.get("source") or {}).get("path")), str) and path
        ]
        resumable, rerunnable = _classify_run_paths(source_paths)
        return {
            "path": str(report_file),
            "report": document,
            "verified_from_raw": False,
            "verification": "schema_only",
            "remote_run_paths": source_paths,
            "resumable_run_paths": resumable,
            "rerunnable_run_paths": rerunnable,
        }
    destination = (
        Path(out).absolute()
        if out
        else state_dir / "reports" / f"export-{uuid.uuid4().hex}"
    )
    document = (
        _write_raw(raw_inputs, destination)
        if verified_from_raw
        else _write_existing(Path(report_path).resolve(), destination)
    )
    validate_document("studio_report", document)
    source_paths = (
        [str(p) for p in raw_inputs]
        if verified_from_raw
        else [
            path
            for run in document.get("runs", [])
            if isinstance((path := (run.get("source") or {}).get("path")), str) and path
        ]
    )
    resumable, rerunnable = _classify_run_paths(source_paths)
    return {
        "path": str(destination / "studio.json"),
        "report": document,
        "verified_from_raw": verified_from_raw,
        "verification": "raw_evidence" if verified_from_raw else "schema_only",
        "remote_run_paths": source_paths,
        "resumable_run_paths": resumable,
        "rerunnable_run_paths": rerunnable,
    }


def export_bytes(result: dict) -> bytes:
    return json.dumps(result["report"], ensure_ascii=False, allow_nan=False).encode("utf-8")
