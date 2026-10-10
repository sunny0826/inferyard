"""Test-only packing of newly generated synthetic originals; no production conversion API."""

import base64
import hashlib
import re

from inferyard.evidence.layout import INPUT_FILES, OBSERVATION_FILES
from inferyard.evidence.storage import json_bytes, read_json, read_jsonl, sha256_file


def digest(value):
    return hashlib.sha256(json_bytes(value)).hexdigest()


def seal_layout(root, run):
    files = {
        path.name: {"sha256": sha256_file(path), "bytes": path.stat().st_size, "derived": False}
        for path in root.iterdir()
        if path.is_file() and path.name != "manifest.json"
    }
    manifest = {
        "schema_version": 3,
        "evidence_layout_version": 2,
        "run_id": run["run_id"],
        "sealed": True,
        "files": files,
        **{
            key: run[key]
            for key in ("kind", "execution_mode", "origin", "experiment_id", "trial_id")
        },
    }
    (root / "manifest.json").write_bytes(json_bytes(manifest))


def pack_layout2(source, destination):
    """Only fixture callers may pack their fresh temporary run into a new directory."""
    destination.mkdir()
    documents = {kind: read_json(source / name) for kind, name in INPUT_FILES.items()}
    run = documents["run"]
    original_sources = [
        source / name
        for name in ("config.input.toml", "config.input.json")
        if (source / name).exists()
    ]
    configuration_source = None
    if original_sources:
        original = original_sources[0]
        raw = original.read_bytes()
        configuration_source = {
            "media_type": "application/toml" if original.suffix == ".toml" else "application/json",
            "encoding": "base64",
            "content": base64.b64encode(raw).decode("ascii"),
            "sha256": hashlib.sha256(raw).hexdigest(),
            "bytes": len(raw),
        }
    inputs = {
        "schema_version": 3,
        "evidence_layout_version": 2,
        "run_id": run["run_id"],
        "documents": {
            kind: {"payload": value, "sha256": digest(value)} for kind, value in documents.items()
        },
        "configuration_source": configuration_source,
        "configuration_source_missing_reason": None if configuration_source else "not_persisted",
    }
    (destination / "inputs.json").write_bytes(json_bytes(inputs))
    rows = []
    for (kind, stage), name in OBSERVATION_FILES.items():
        if not (source / name).exists():
            continue
        stage = stage or ("post_probe" if kind == "identity" else "post_idle")
        payload = read_json(source / name)
        rows.append(
            {
                "schema_version": 3,
                "evidence_layout_version": 2,
                "run_id": run["run_id"],
                "trial_id": run["trial_id"],
                "seq": len(rows) + 1,
                "kind": kind,
                "stage": stage,
                "key": None,
                "clock_id": "fixture-clock",
                "monotonic_ns": len(rows),
                "utc": "2026-10-10T00:00:00Z",
                "payload": payload,
                "payload_sha256": digest(payload),
            }
        )
    (destination / "observations.jsonl").write_bytes(b"".join(map(json_bytes, rows)))
    events, _ = read_jsonl(source / "events.jsonl")
    started = {
        event["request_id"]: event for event in events if event["event_type"] == "request_started"
    }
    prepared = []
    for path in sorted(source.glob("*.request.json")):
        match = re.fullmatch(r"(.+)__([0-9]+)__(probe|warmup|formal)\.request\.json", path.name)
        if match is None:
            raise ValueError("fixture is not a current original run")
        ordinal, phase = int(match[2]), match[3]
        key = next(
            (
                key
                for key, event in started.items()
                if event["phase"] == phase and key.endswith((f"-{ordinal}", f":{ordinal}"))
            ),
            f"{run['run_id']}-{phase}-{ordinal}",
        )
        body = read_json(path)
        prepared.append(
            {
                "schema_version": 3,
                "evidence_layout_version": 2,
                "run_id": run["run_id"],
                "trial_id": run["trial_id"],
                "request_id": key,
                "ordinal": ordinal,
                "phase": phase,
                "body": body,
                "body_sha256": digest(body),
            }
        )
    (destination / "request-bodies.jsonl").write_bytes(b"".join(map(json_bytes, prepared)))
    for name in ("events.jsonl", "memory.jsonl", "environment.jsonl", "schedule.jsonl"):
        (destination / name).write_bytes((source / name).read_bytes())
    if (source / "manifest.json").exists():
        seal_layout(destination, run)
    return destination
