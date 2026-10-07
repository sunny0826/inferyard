"""Committed legacy evidence remains byte-identical after portable archival copying."""

import importlib.util
import subprocess
from pathlib import Path

import pytest

from inferyard.evidence.ledger import read_trial
from inferyard.evidence.migration import migrate_run
from inferyard.evidence.migration_source import load_source
from inferyard.evidence.storage import (
    EvidenceError,
    json_bytes,
    local_file,
    read_json,
    sha256_file,
)

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location(
    "prepare_windows_evidence", ROOT / "scripts/prepare_windows_evidence.py"
)
module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(module)
PACKET = "validation/synthetic"


@pytest.fixture(params=[1, 2])
def repository(tmp_path, request):
    """Commit raw fixture blobs, including legacy names Windows cannot check out."""
    root = tmp_path / "repository"
    root.mkdir()

    def git(*args, data=None):
        return subprocess.check_output(["git", *args], cwd=root, input=data)

    git("init", "--quiet")
    # Only an object/index fixture: never check these legacy colon names out on Windows.
    git("config", "core.protectNTFS", "false")
    source = ROOT / f"tests/fixtures/legacy/synthetic-v{request.param}"
    manifest = read_json(source / "manifest.json")
    files = {path.name: path.read_bytes() for path in source.iterdir()}
    # Restore old request names only in the temporary Git tree; request bytes stay intact.
    for name in list(manifest["files"]):
        if not name.endswith(".request.json"):
            continue
        run_id, ordinal, phase = name.removesuffix(".request.json").split("__")
        original = f"{run_id}:{phase}:{int(ordinal)}.request.json"
        files[original] = files.pop(name)
        manifest["files"][original] = manifest["files"].pop(name)
    files["manifest.json"] = json_bytes(manifest)
    for name, raw in files.items():
        blob = git("hash-object", "-w", "--stdin", data=raw).decode().strip()
        git("update-index", "--add", "--cacheinfo", f"100644,{blob},{PACKET}/{name}")
    tree = git("write-tree").decode().strip()
    commit = (
        git(
            "-c",
            "user.name=Fixture",
            "-c",
            "user.email=fixture@example.invalid",
            "-c",
            "commit.gpgsign=false",
            "commit-tree",
            tree,
            "-m",
            "Synthetic legacy packet",
        )
        .decode()
        .strip()
    )
    git("update-ref", "HEAD", commit)
    return root


def test_original_manifest_and_request_bytes_survive_aliases_and_replay(tmp_path, repository):
    out = tmp_path / "portable"
    receipt = module.copy_packet(PACKET, out, repository=repository)
    assert not receipt["hardware_qualification_added"]
    assert (out / "manifest.json").read_bytes() == subprocess.check_output(
        ["git", "show", receipt["git_revision"] + ":" + PACKET + "/manifest.json"],
        cwd=repository,
    )
    load_source(out)
    manifest = read_json(out / "manifest.json")
    assert len(receipt["aliases"]) == 8
    for original, alias in receipt["aliases"].items():
        assert ":" not in alias and "__" in alias and "/" not in alias
        assert local_file(out, original) == out / alias
        assert sha256_file(out / alias) == manifest["files"][original]["sha256"]
    bodies = [local_file(out, name) for name in manifest["files"] if name.endswith(".request.json")]
    assert bodies
    for body in bodies:
        assert ":" not in body.name and "__" in body.name
    migrated = tmp_path / "current"
    migrate_run(out, migrated)
    data = read_trial(migrated)
    assert data["summary"]["counts"]["completed"] == 3
    with pytest.raises(FileExistsError):
        module.copy_packet(PACKET, out, repository=repository)
    bodies[0].write_bytes(b"corrupt")
    with pytest.raises(EvidenceError, match="migration_source_hash_mismatch"):
        load_source(out)


@pytest.mark.parametrize("name", ["../validation", "/validation", "src", "validation/../src"])
def test_copy_rejects_nonvalidation_roots_before_writing(tmp_path, name):
    with pytest.raises(EvidenceError):
        module.copy_packet(name, tmp_path / "output")
    assert not (tmp_path / "output").exists()
