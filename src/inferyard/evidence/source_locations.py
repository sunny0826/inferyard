"""Explicit external-source locators; package-local files use local_file instead."""

import os
from pathlib import Path

from inferyard.evidence.storage import EvidenceError


def relative_source(path, out):
    try:
        return Path(os.path.relpath(path, out)).as_posix()
    except ValueError:
        return str(Path(path).resolve())


def resolve_source(root, value, source_roots=()):
    if type(value) is not str or not value or "\0" in value:
        raise EvidenceError("invalid_source_locator")
    path = Path(value)
    path = (root / path).resolve() if not path.is_absolute() else path.resolve()
    matches = [(old, new) for old, new in source_roots if path.is_relative_to(old)]
    if len(matches) > 1:
        raise EvidenceError("ambiguous_source_root")
    if matches:
        old, new = matches[0]
        path = new / path.relative_to(old)
    return path


def comparison_locations(value):
    yield from value.get("source_runs", [])
    for proof in value.get("performance_evidence", []):
        yield proof["source"]


def relative_comparison(value, out):
    for ref in comparison_locations(value):
        ref["path"] = relative_source(ref["path"], out)


def retain_comparison_locations(expected, saved):
    refs, original = list(comparison_locations(expected)), list(comparison_locations(saved))
    if len(refs) != len(original):
        raise EvidenceError("comparison_sources_mismatch")
    for ref, stored in zip(refs, original, strict=True):
        ref["path"] = stored["path"]
