"""Distribution gates reject drift and unapproved development artifacts."""

import hashlib
import json
import os
import subprocess
import sys
import tomllib
import zipfile
from pathlib import Path

import pytest

from scripts.check_distribution import inspect_wheel, source_resources, validate_resources
from scripts.export_runtime_constraints import validate
from scripts.verify_release_candidate import checked_file, verify

ROOT = Path(__file__).resolve().parents[2]


def test_constraint_export_rejects_missing_transitive_and_marker_overlaps():
    lock = tomllib.loads((ROOT / "uv.lock").read_text())
    project = tomllib.loads((ROOT / "pyproject.toml").read_text())["project"]
    with pytest.raises(ValueError, match="runtime_closure_mismatch"):
        validate("httpx==0.28.1\n", lock, project)
    with pytest.raises(ValueError, match="overlapping_constraints"):
        validate("httpx==0.28.1\nhttpx==0.28.1; sys_platform == 'linux'", lock, project)
    with pytest.raises(ValueError, match="exact_registry"):
        validate("httpx @ https://example.test/httpx.whl", lock, project)


def test_package_source_allowlist_rejects_accidental_credentials(tmp_path):
    package = tmp_path / "src/inferyard"
    package.mkdir(parents=True)
    (package / ".env").write_text("synthetic secret")
    with pytest.raises(ValueError, match="unexpected_source_resource"):
        source_resources(tmp_path)


def test_package_source_includes_context_speed_bundle():
    resources = source_resources(ROOT)
    name = "data/community/bundles/zh-context-speed.json"
    assert (
        resources[name]
        == hashlib.sha256((ROOT / "bundles/zh-context-speed.json").read_bytes()).hexdigest()
    )
    validate_resources(resources)
