"""Explicit mutations of disposable layout2 specimens, never original run bytes."""

import hashlib

from inferyard.evidence.storage import json_bytes, read_json, read_jsonl


def digest(value):
    return hashlib.sha256(json_bytes(value)).hexdigest()


def reseal(root, *names):
    path = root / "manifest.json"
    if not path.exists():
        return
    manifest = read_json(path)
    for name in names:
        raw = (root / name).read_bytes()
        manifest["files"][name].update(bytes=len(raw), sha256=hashlib.sha256(raw).hexdigest())
    path.write_bytes(json_bytes(manifest))


def change_input(root, kind, change, *, sealed=False):
    path = root / "inputs.json"
    value = read_json(path)
    member = value["documents"][kind]
    change(member["payload"])
    member["sha256"] = digest(member["payload"])
    path.write_bytes(json_bytes(value))
    if sealed:
        reseal(root, "inputs.json")


def change_observation(
    root, kind, change=None, *, stage=None, key=None, remove=False, sealed=False
):
    path = root / "observations.jsonl"
    rows, tails = read_jsonl(path)
    assert not tails
    matches = [
        row
        for row in rows
        if row["kind"] == kind
        and (stage is None or row["stage"] == stage)
        and (key is None or row["key"] == key)
    ]
    assert matches
    for row in matches:
        if remove:
            rows.remove(row)
        else:
            change(row["payload"])
            row["payload_sha256"] = digest(row["payload"])
    for seq, row in enumerate(rows, 1):
        row["seq"] = seq
    path.write_bytes(b"".join(json_bytes(row) for row in rows))
    if sealed:
        reseal(root, "observations.jsonl")


def change_request(root, change=None, *, request_id=None, ordinal=None, remove=False, sealed=False):
    path = root / "request-bodies.jsonl"
    rows, tails = read_jsonl(path)
    assert not tails
    row = next(
        row
        for row in rows
        if (request_id is None or row["request_id"] == request_id)
        and (ordinal is None or row["ordinal"] == ordinal)
    )
    if remove:
        rows.remove(row)
    else:
        change(row["body"])
        row["body_sha256"] = digest(row["body"])
    path.write_bytes(b"".join(json_bytes(row) for row in rows))
    if sealed:
        reseal(root, "request-bodies.jsonl")
