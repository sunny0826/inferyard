"""Offline corpus identity, reproducibility and real admission/refusal paths."""

import hashlib
import json
import subprocess
import sys
from pathlib import Path

import pytest

from inferyard.contracts.bundle import content_hash, validate_bundle
from inferyard.config.bundle import require_review
from inferyard.contracts.bundle_review import case_content_hash
from inferyard.errors import ContractError
from scripts.build_context_speed_bundle import (
    BUNDLE_NAME,
    MARKDOWN_NAME,
    REVIEW_NAME,
    build,
    outputs,
    write_outputs,
)

ROOT = Path(__file__).resolve().parents[2]


def test_repeatable_bytes_and_checked_in_artifacts():
    first = outputs()
    assert outputs() == first
    for name, raw in first.items():
        actual = (ROOT / "bundles" / name).read_bytes()
        if name == BUNDLE_NAME:
            approved = json.loads(actual)
            require_review(approved)
            approved["review_records"] = []
            assert approved == json.loads(raw)
        else:
            assert actual == raw
