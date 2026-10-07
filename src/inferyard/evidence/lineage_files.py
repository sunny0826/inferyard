"""Streaming local artifact identity checks, separate from provenance authentication."""

import hashlib
import os
import stat
from pathlib import Path

from inferyard.config.lineage_records import (
    CONVERSION,
    RECORDS,
    parse_record,
    validate_records,
)
from inferyard.config.model_lineage import LINEAGE
from inferyard.contracts.validation import ContractError, _validate
from inferyard.evidence.storage import json_bytes
from inferyard.platforms.platform_io import open_nofollow


def fingerprint(info):
    # Windows stat() and fstat() expose different deprecated st_ctime meanings
    # in Python 3.14. The explicit birth time is consistent for both APIs.
    creation = info.st_birthtime_ns if os.name == "nt" else info.st_ctime_ns
    return (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, creation)


def verify_file(root, reference, expected):
    result = {"expected_sha256": expected, "sha256": None, "bytes": None, "status": "unknown"}
    if reference is None:
        return {**result, "reason": "file_binding_missing"}
    if not isinstance(reference, str) or not reference:
        raise ContractError("lineage.files", "requires nonempty relative file paths")
    relative = Path(reference)
    if relative.is_absolute() or ".." in relative.parts:
        raise ContractError("lineage.files", "file path escapes root")
    root = Path(root).resolve()
    try:
        path = (root / relative).resolve()
        if not path.is_relative_to(root):
            raise ContractError("lineage.files", "file path escapes root")
        fd = open_nofollow(path, os.O_RDONLY | getattr(os, "O_NONBLOCK", 0))
        before = os.fstat(fd)
        if not stat.S_ISREG(before.st_mode):
            os.close(fd)
            return {**result, "reason": "not_regular_file"}
        with os.fdopen(fd, "rb") as stream:
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
            after = os.fstat(stream.fileno())
            linked = path.stat(follow_symlinks=False)
        if fingerprint(before) != fingerprint(after) or fingerprint(after) != fingerprint(linked):
            return {**result, "reason": "file_changed_during_hash"}
    except FileNotFoundError:
        return {**result, "reason": "file_missing_or_removed"}
    except OSError, RuntimeError:
        return {**result, "reason": "file_unreadable"}
    return {
        **result,
        "sha256": digest,
        "bytes": after.st_size,
        "status": "verified" if digest == expected else "mismatch",
        "reason": None if digest == expected else "file_hash_mismatch",
    }


def verify_lineage_files(workload, bindings, root):
    declaration = workload.get("model_lineage")
    _validate(declaration, LINEAGE, "lineage.declaration")
    records = workload.get("model_lineage_records")
    _validate(records, RECORDS, "lineage.records")
    validate_records(workload)
    expected = {"base:" + a["name"]: a["sha256"] for a in declaration["base_artifacts"]}
    for key in (
        "tokenizer",
        "conversion_tool",
        "conversion_recipe",
        "quantization_tool",
        "quantization_recipe",
        "output",
    ):
        expected[key] = declaration[key + "_sha256"]
    expected["intermediate"] = parse_record(records["conversion"], CONVERSION)["output_sha256"]
    if not isinstance(bindings, dict) or set(bindings) - set(expected):
        raise ContractError("lineage.files", "unknown file binding")
    rows = [
        {"artifact": key, **verify_file(root, bindings.get(key), digest)}
        for key, digest in sorted(expected.items())
    ]
    return {
        "definition": "local_lineage_file_identity.v1",
        "workload_sha256": hashlib.sha256(json_bytes(workload)).hexdigest(),
        "bindings_sha256": hashlib.sha256(json_bytes(bindings)).hexdigest(),
        "files": rows,
        "all_files_verified": all(row["status"] == "verified" for row in rows),
        "lineage_authenticated": False,
        "comparison_eligible": False,
        "limitations": [
            "file_bytes_match_declared_hashes_only",
            "does_not_prove_execution_or_repository_revision_membership",
            "does_not_authenticate_receipt_author_or_tool_behavior",
            "no_quantization_comparison_authorization",
        ],
    }
