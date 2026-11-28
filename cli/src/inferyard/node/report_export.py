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
        return {
            "path": str(report_file),
            "report": document,
            "verified_from_raw": False,
            "verification": "schema_only",
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
    return {
        "path": str(destination / "studio.json"),
        "report": document,
        "verified_from_raw": verified_from_raw,
        "verification": "raw_evidence" if verified_from_raw else "schema_only",
    }


def export_bytes(result: dict) -> bytes:
    return json.dumps(result["report"], ensure_ascii=False, allow_nan=False).encode("utf-8")
