"""Validate the one-call proof emitted by a prepared Studio recovery controller."""

from __future__ import annotations

import json
import os
from pathlib import Path

from .state import DevError, file_sha, inside, read_json


def validate_proof(
    output: str, state: Path, controller: Path, target: Path, hashes: tuple[str, str]
) -> str:
    def pairs(entries):
        value = {}
        for key, item in entries:
            if key in value:
                raise ValueError("duplicate proof field")
            value[key] = item
        return value

    def invalid_constant(value):
        raise ValueError(f"nonfinite proof value: {value}")

    try:
        proof = json.loads(output, object_pairs_hook=pairs, parse_constant=invalid_constant)
    except (TypeError, ValueError) as exc:
        raise DevError("Invalid Studio recovery proof") from exc
    fields = {
        "kind",
        "outcome",
        "controller_manifest",
        "controller_sha256",
        "target_manifest",
        "target_sha256",
        "child_sha256",
        "pid",
    }
    if not isinstance(proof, dict) or set(proof) != fields:
        raise DevError("Incomplete Studio recovery proof")
    maximum = 2**32 - 1 if os.name == "nt" else 2**31 - 1
    if (
        proof["kind"] != "studio_dev_recovery.v1"
        or proof["outcome"] not in ("recovered_missing_session", "session_present")
        or proof["controller_manifest"] != str(controller)
        or proof["target_manifest"] != str(target)
        or proof["controller_sha256"] != hashes[0]
        or proof["target_sha256"] != hashes[1]
        or type(proof["pid"]) is not int
        or not 0 < proof["pid"] <= maximum
        or file_sha(controller) != hashes[0]
        or file_sha(target) != hashes[1]
    ):
        raise DevError("Studio recovery proof does not match its inputs")
    child_sha = proof["child_sha256"]
    if child_sha != "":
        child = inside(target.parent / "child.json", state / "launches")
        data = read_json(child)
        if (
            not isinstance(child_sha, str)
            or file_sha(child) != child_sha
            or set(data) != {"pid"}
            or type(data["pid"]) is not int
            or data["pid"] != proof["pid"]
        ):
            raise DevError("Studio recovery proof does not match the child snapshot")
    elif proof["outcome"] == "recovered_missing_session":
        raise DevError("Missing child snapshot in Studio recovery proof")
    return proof["outcome"]
