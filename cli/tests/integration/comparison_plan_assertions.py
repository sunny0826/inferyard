"""Evidence mutation helpers for comparison plan tests."""

import hashlib
import json
from collections.abc import Callable
from pathlib import Path

from inferyard.analysis.ledger import read_trial
from inferyard.contracts.comparison_types import JSONRecord
from inferyard.contracts.validation import json_bytes


def _rehash_manifest(run_dir: Path, filename: str) -> None:
    manifest_path = run_dir / "manifest.json"
    manifest: JSONRecord = json.loads(manifest_path.read_text())
    raw = (run_dir / filename).read_bytes()
    entry = manifest["files"][filename]
    entry["sha256"] = hashlib.sha256(raw).hexdigest()
    entry["bytes"] = len(raw)
    manifest_path.write_bytes(json_bytes(manifest))


def run_paths(batch: Path) -> dict[str, Path]:
    return {
        read_trial(path)["run"]["trial_id"]: path
        for path in (batch / "runs").iterdir()
    }


def _retrial_jsonl(run_dir: Path, old: str, new: str) -> None:
    for path in run_dir.glob("*.jsonl"):
        rows: list[JSONRecord] = [json.loads(line) for line in path.read_text().splitlines()]
        changed = False
        for row in rows:
            if row.get("trial_id") == old:
                row["trial_id"] = new
                changed = True
        if changed:
            path.write_bytes(b"".join(json_bytes(row) for row in rows))
            _rehash_manifest(run_dir, path.name)


def mutate_run_documents(
    run_dir: Path, mutate: Callable[[JSONRecord, JSONRecord], None]
) -> None:
    inputs_path = run_dir / "inputs.json"
    inputs: JSONRecord = json.loads(inputs_path.read_text())
    run = inputs["documents"]["run"]["payload"]
    selection = inputs["documents"]["selection"]["payload"]
    old_trial = run["trial_id"]
    mutate(run, selection)
    if run["trial_id"] != old_trial:
        _retrial_jsonl(run_dir, old_trial, run["trial_id"])
    manifest_path = run_dir / "manifest.json"
    manifest: JSONRecord = json.loads(manifest_path.read_text())
    for key in ("run_id", "experiment_id", "trial_id", "origin", "kind", "execution_mode"):
        manifest[key] = run[key]
    manifest_path.write_bytes(json_bytes(manifest))
    for kind in ("run", "selection"):
        payload = inputs["documents"][kind]["payload"]
        inputs["documents"][kind]["sha256"] = hashlib.sha256(json_bytes(payload)).hexdigest()
    inputs_path.write_bytes(json_bytes(inputs))
    _rehash_manifest(run_dir, "inputs.json")
