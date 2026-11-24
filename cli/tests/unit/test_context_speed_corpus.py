"""Offline corpus identity and checked-in release artifact reproducibility."""

import json
from pathlib import Path

from inferyard.config.bundle import require_review
from scripts.build_context_speed_bundle import (
    BUNDLE_NAME,
    MARKDOWN_NAME,
    REVIEW_NAME,
    outputs,
)

ROOT = Path(__file__).resolve().parents[2]


def test_repeatable_bytes_and_checked_in_artifacts() -> None:
    first = outputs()
    assert outputs() == first
    assert set(first) == {BUNDLE_NAME, REVIEW_NAME, MARKDOWN_NAME}
    approved = json.loads((ROOT / "bundles" / BUNDLE_NAME).read_bytes())
    require_review(approved)
    approved["review_records"] = []
    assert approved == json.loads(first[BUNDLE_NAME])
