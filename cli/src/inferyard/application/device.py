"""Command result for device inspection, including the optional written snapshot."""

from pathlib import Path

from inferyard.application.types import CommandResult


def execute(request):
    from inferyard.contracts.validation import json_bytes
    from inferyard.evidence.storage import atomic_bytes
    from inferyard.platforms.device_preflight import existing_ancestor, inspect_device

    report = inspect_device(
        request.models_roots or [Path("artifacts")],
        model_path=request.model_path,
        disk_path=existing_ancestor(request.out or Path.cwd()),
    )
    if request.out:
        request.out.mkdir(parents=True, exist_ok=False)
        atomic_bytes(request.out / "device-preflight.json", json_bytes(report))
    accepted = report["recommendation"]["status"] == "recommended"
    inspected = not report["models"] and not request.models_roots and request.model_path is None
    status = "recommended" if accepted else "inspected" if inspected else "blocked"
    return (0 if accepted or inspected else 2), CommandResult(
        request.command,
        status,
        "complete",
        evidence_dir=str(request.out) if request.out else None,
        details=report,
        limitations=(report["recommendation"]["reason"],) if status == "blocked" else (),
    )
