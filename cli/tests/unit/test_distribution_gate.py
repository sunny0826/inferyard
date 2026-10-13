"""Distribution gates reject drift and unapproved development artifacts."""

import hashlib
import json
import tomllib
import zipfile
from pathlib import Path

import pytest

from scripts.check_distribution import inspect_wheel, source_resources
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


def test_wheel_cannot_hide_machine_files_in_dist_info(tmp_path):
    wheel = tmp_path / "bad.whl"
    with zipfile.ZipFile(wheel, "w") as archive:
        archive.writestr("inferyard-0.0.1.dist-info/secret.txt", "private")
    with pytest.raises(ValueError, match="unexpected_wheel_member"):
        inspect_wheel(wheel, {}, {"version": "0.0.1"})


def test_package_source_allowlist_rejects_accidental_credentials(tmp_path):
    package = tmp_path / "src/inferyard"
    package.mkdir(parents=True)
    (package / ".env").write_text("synthetic secret")
    with pytest.raises(ValueError, match="unexpected_source_resource"):
        source_resources(tmp_path)


def test_release_gate_refuses_dev_build_and_changed_acceptance_bytes(tmp_path):
    manifest = tmp_path / "manifest.json"
    raw = json.dumps({"kind": "community_distribution.v1", "release_approved": False}).encode()
    manifest.write_bytes(raw)
    digest = hashlib.sha256(raw).hexdigest()
    with pytest.raises(ValueError, match="not_approved"):
        verify(manifest, digest, "a" * 40)
    with pytest.raises(ValueError, match="manifest_hash"):
        verify(manifest, "b" * 64, "a" * 40)


@pytest.mark.parametrize("name", ["../secret", "/secret", "C:/secret", "a\\b", "a/../b"])
def test_release_paths_cannot_escape(tmp_path, name):
    with pytest.raises(ValueError, match="unsafe_candidate"):
        checked_file(tmp_path, {"path": name})


def test_release_artifact_bytes_checked_not_only_claimed(tmp_path):
    (tmp_path / "wheel.whl").write_bytes(b"changed")
    with pytest.raises(ValueError, match="hash_mismatch"):
        checked_file(tmp_path, {"path": "wheel.whl", "bytes": 7, "sha256": "a" * 64})
