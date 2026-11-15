"""Neutral source reads shared by comparisons, reports, exports and rescoring."""

import hashlib

from inferyard.analysis.ledger import read_trial
from inferyard.contracts.validation import json_bytes
from inferyard.evidence.trial_evidence import verify_reference


def _reference_identity(ref):
    if (
        type(ref) is not dict
        or set(ref) not in ({"path", "sha256"}, {"path", "sha256", "member"})
        or type(ref["path"]) is not str
        or type(ref["sha256"]) is not str
    ):
        return json_bytes(ref)
    physical = (ref["path"], ref["sha256"])
    if "member" not in ref:
        return physical
    member = ref["member"]
    if type(member) is dict and type(member.get("sha256")) is str:
        kind = member.get("kind")
        field = {"input": "name", "observation": "seq", "request": "request_id"}.get(
            kind if type(kind) is str else None
        )
        if field is not None and set(member) == {"kind", field, "sha256"}:
            value = member[field]
            if type(value) is (int if kind == "observation" else str):
                return (*physical, kind, value, member["sha256"])
    return json_bytes(ref)


def verify_metric_references(root, metrics, *, reads=None):
    """Bind each distinct physical/member reference once within this source read."""
    checked, member_reads = set(), {}
    for metric in metrics:
        for ref in metric["evidence_refs"]:
            identity = _reference_identity(ref)
            if identity not in checked:
                verify_reference(root, ref, reads=reads, readers=member_reads)
                checked.add(identity)


def comparison_input(root, *, bind_unsealed=False, metadata=None):
    metadata = {} if metadata is None else metadata
    data = read_trial(root, metadata=metadata)
    verify_metric_references(
        root, data["summary"]["metric_observations"], reads=metadata["evidence"]
    )
    data.update(
        {key: metadata[key] for key in ("identity", "environment_start", "first_event_utc")}
    )
    ref = {
        "run_id": data["run"]["run_id"],
        "path": str(root.resolve()),
        "manifest_sha256": metadata["manifest_sha256"],
    }
    if bind_unsealed and ref["manifest_sha256"] is None:
        ref["unsealed_content_sha256"] = hashlib.sha256(json_bytes(data)).hexdigest()
    return data, ref
