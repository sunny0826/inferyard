"""Command-local batch state, not an offline integrity or measurement verification."""

import hashlib
import os

from inferyard.contracts.validation import ContractError, strict_json_loads, validate_document
from inferyard.evidence.capacity_stop import validate_capacity_stop
from inferyard.evidence.duration_ledger import duration_summary
from inferyard.evidence.event_ledger import PROJECTION_EVENTS, reduce_events
from inferyard.evidence.formats import UnsupportedFormat, require_core
from inferyard.evidence.journal import trial_for
from inferyard.evidence.lab import verify_lab
from inferyard.evidence.native import observation_evidence
from inferyard.evidence.request_snapshots import require_current_sources
from inferyard.evidence.service_drain import service_drain
from inferyard.evidence.storage import EvidenceError, local_file
from inferyard.evidence.trial_reads import TrialReads
from inferyard.evidence.trial_summary import trial_summary


def _manifest(root, reads):
    if not reads.exists("manifest.json"):
        return ["manifest_missing_unsealed_run"]
    if "manifest.json" in reads.errors:
        raise reads.errors["manifest.json"]
    manifest = reads.manifest
    if (
        type(manifest) is not dict
        or manifest.get("sealed") is not True
        or type(manifest.get("files")) is not dict
    ):
        raise EvidenceError("invalid_manifest")
    for name, info in manifest["files"].items():
        if name == "manifest.json" or type(info) is not dict:
            raise EvidenceError("invalid_manifest_entry")
        path = local_file(root, name)
        # Keep inventory/path checks, but do not open every original to hash it.
        if name not in ("summary.json", "report.html") and (
            not path.is_file() or path.stat().st_size != info.get("bytes")
        ):
            raise EvidenceError("original_evidence_hash_mismatch")
    require_core(manifest, "manifest")
    try:
        validate_document("manifest", manifest)
    except ContractError as exc:
        raise EvidenceError("invalid_manifest") from exc
    if "requests.jsonl" in manifest["files"]:
        raise UnsupportedFormat("run.source", "requests.jsonl", ("events.jsonl",))
    return []


def _events(root, reads, retained, tails):
    digest, size, corrupt = hashlib.sha256(), 0, False
    try:
        with local_file(root, "events.jsonl").open("rb") as stream:
            for line in stream:
                digest.update(line)
                if not line.endswith(b"\n"):
                    tails.append(f"truncated_tail_at_byte:{size}")
                else:
                    try:
                        record = strict_json_loads(line.decode("utf-8"))
                        if type(record) is not dict:
                            raise ContractError("record", "invalid_jsonl_record")
                    except UnicodeError, ContractError:
                        corrupt = True
                    else:
                        # All lines are parsed strictly; only consumed kinds get semantic
                        # validation. The reducer sees original line positions, not renumbered
                        # selected events. No content/wire chunks survive this iteration.
                        if not corrupt:
                            if (
                                type(record.get("event_type")) is str
                                and record["event_type"] in PROJECTION_EVENTS
                            ):
                                retained.append(record)
                            yield record
                size += len(line)
    except OSError as exc:
        raise EvidenceError("corrupt_jsonl_evidence") from exc
    reads.hashes["events.jsonl"] = digest.hexdigest()
    if corrupt:
        # read_trial gives a broken original seal precedence over JSON errors.
        # Reuse the digest already required for parent lineage; no second file read.
        info = (reads.manifest or {}).get("files", {}).get("events.jsonl")
        if info and (info["sha256"] != digest.hexdigest() or info["bytes"] != size):
            raise EvidenceError("original_evidence_hash_mismatch")
        raise EvidenceError("corrupt_jsonl_evidence")


def _memory_tail(root):
    # read_trial completeness includes memory's truncated-tail flag. Inspect only
    # that flag, never parse/hash the samples or reconstruct resource observations.
    try:
        with local_file(root, "memory.jsonl").open("rb") as stream:
            size = stream.seek(0, os.SEEK_END)
            if size:
                stream.seek(-1, os.SEEK_END)
                if stream.read(1) != b"\n":
                    return ["memory_truncated_tail"]
    except OSError as exc:
        raise EvidenceError("corrupt_jsonl_evidence") from exc
    return []


def read_run_projection(root):
    from inferyard.evidence.ledger import CORE, _inputs

    root = root.resolve()
    reads = TrialReads(root)
    for name in CORE - {"events.jsonl", "memory.jsonl"}:
        reads.json(name)
    limits = _manifest(root, reads)
    files = reads.manifest["files"] if reads.manifest is not None else {}
    require_current_sources(root, files)
    documents, limits = _inputs(root, reads, limits)
    if reads.errors:
        raise next(iter(reads.errors.values()))
    run, selection = documents["run"], documents["selection"]
    trial = trial_for(documents["plan"], run["trial_id"])
    workload = next(
        w
        for w in documents["plan"]["experiment"]["workloads"]
        if w["workload_id"] == trial["workload_id"]
    )
    duration = workload["protocol"] if workload["protocol"]["kind"] == "duration" else None
    retained, tails = [], []
    requests, _, stopped = reduce_events(
        _events(root, reads, retained, tails),
        run,
        selection,
        {c["case_id"]: c for c in documents["bundle"]["cases"]},
        duration=duration,
        _projection=True,
    )
    validate_capacity_stop(documents["plan"]["experiment"], requests, stopped)
    window = (
        duration_summary(duration, selection["case_ids"], retained, requests) if duration else None
    )
    # Batch writers do not persist summary.json. Even when present it is a derived,
    # replaceable file: read_trial reconstructs it, so history must do the same.
    summary = trial_summary(
        run, requests, stopped, limits + tails + _memory_tail(root), window, duration
    )
    verify_lab(
        root, documents["config"], documents["bundle"], retained, reads=reads, _projection=True
    )
    observation = observation_evidence(root, documents["config"])
    fields = (
        "case_id",
        "execution_state",
        "category",
        "quality_state",
        "score",
        "t_send_ns",
        "t_terminal_ns",
    )
    return {
        **documents,
        "requests": [{k: row[k] for k in fields if k in row} for row in requests],
        "summary": summary,
        "events_sha256": reads.hashes["events.jsonl"],
        "manifest_sealed": reads.manifest is not None,
        "service_drain": service_drain(
            retained, sealed="events.jsonl" in files, truncated=bool(tails), observation=observation
        ),
    }
