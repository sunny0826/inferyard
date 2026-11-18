"""Partial evidence cannot be promoted when reading continuation metadata."""

import pytest

from inferyard.analysis.ledger import read_run_projection
from inferyard.contracts.validation import json_bytes
from inferyard.evidence.storage import read_json, sha256_file
from tests.unit.test_ledger import attempt, finish, journal
from tests.unit.test_ledger import setup as ledger_setup

setup = ledger_setup


def reseal_file(path, name):
    manifest = read_json(path / "manifest.json")
    manifest["files"][name]["sha256"] = sha256_file(path / name)
    manifest["files"][name]["bytes"] = (path / name).stat().st_size
    (path / "manifest.json").write_bytes(json_bytes(manifest))
