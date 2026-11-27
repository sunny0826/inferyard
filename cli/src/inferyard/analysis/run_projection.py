"""Command-local batch state, not an offline integrity or measurement verification."""

import hashlib
import os

from inferyard.contracts.validation import strict_json_loads, validate_document
from inferyard.errors import ContractError, EvidenceError, UnsupportedFormat
from inferyard.evidence.capacity_stop import validate_capacity_stop
from inferyard.analysis.duration_ledger import duration_summary
from inferyard.evidence.event_ledger import PROJECTION_EVENTS, reduce_events
from inferyard.evidence.formats import require_core
from inferyard.evidence.lab import verify_lab
from inferyard.evidence.request_snapshots import require_current_sources
from inferyard.evidence.service_drain import service_drain
from inferyard.evidence.storage import local_file
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.analysis.trial_summary import trial_summary


def _manifest(root, reads):
    if reads.manifest is None and "manifest.json" not in reads.errors:
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
        info = (reads.manifest or {}).get("files", {}).get("events.jsonl")
        if info and (info["sha256"] != digest.hexdigest() or info["bytes"] != size):
            raise EvidenceError("original_evidence_hash_mismatch")
        raise EvidenceError("corrupt_jsonl_evidence")


def _memory_tail(root):
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
    from inferyard.analysis.ledger import _inputs, preload_inputs

    root = root.resolve()
    reads = TrialEvidence(root, projection=True)
    preload_inputs(reads)
    limits = _manifest(root, reads)
    files = reads.manifest["files"] if reads.manifest is not None else {}
    require_current_sources(root, files)
    documents, limits, trial, workload = _inputs(root, reads, limits)
    if documents["config"].get("definition") == "local-engine-config.v1":
        from inferyard.analysis.ledger import read_trial

        metadata = {}
        data = read_trial(root, metadata=metadata)
        return {
            **data,
            "identity": metadata["identity"],
            "manifest_sealed": metadata["manifest"] is not None,
            "service_drain": metadata.get("service_drain"),
        }
    if reads.errors:
        raise next(iter(reads.errors.values()))
    run, selection = documents["run"], documents["selection"]
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
    summary = trial_summary(
        run, requests, stopped, limits + tails + _memory_tail(root), window, duration
    )
    _, observation = verify_lab(
        root, documents["config"], documents["bundle"], retained, reads=reads, _projection=True
    )
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


def read_unsealed_trial(root):
    """Project an unsealed or actively running trial without finalized seal observations."""
    from inferyard import SCHEMA_VERSION
    from inferyard.analysis.ledger import _inputs, preload_inputs

    root = root.resolve()
    reads = TrialEvidence(root, projection=True)
    preload_inputs(reads)
    limits = _manifest(root, reads)
    require_current_sources(root, {})
    documents, limits, _, workload = _inputs(root, reads, limits)
    reads.check_input_errors()
    reads._observations.scan()
    reads._prepared.scan()
    if reads.errors:
        raise next(iter(reads.errors.values()))
    run, selection = documents["run"], documents["selection"]
    duration = workload["protocol"] if workload["protocol"]["kind"] == "duration" else None
    retained, tails = [], []
    events = _events(root, reads, retained, tails) if reads.exists("events.jsonl") else ()
    requests, clock, stopped = reduce_events(
        events,
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
    mem_tails = []
    if reads.exists("memory.jsonl"):
        samples, mem_tails = reads.checked_jsonl("memory.jsonl")
        for i, sample in enumerate(samples, 1):
            try:
                require_core(sample, "sample")
                validate_document("sample", sample)
            except ContractError as exc:
                raise EvidenceError("invalid_sample") from exc
            if (
                sample["schema_version"] != SCHEMA_VERSION
                or sample["seq"] != i
                or any(sample[k] != run[k] for k in ("run_id", "trial_id", "experiment_id"))
                or (clock is not None and sample["clock_id"] != clock)
            ):
                raise EvidenceError("sample_trial_identity_mismatch")
    container_tails = [
        f"{name}:{tail}" for name, items in reads.container_tails.items() for tail in items
    ]
    summary = trial_summary(
        run, requests, stopped, limits + tails + mem_tails + container_tails, window, duration
    )
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
    }
