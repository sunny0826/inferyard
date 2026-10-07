"""Existing sealed bodies migrate without losing identity or accepting corrupt copies."""

import hashlib
import importlib.util
from pathlib import Path

import pytest

from inferyard.evidence.request_snapshots import snapshot_filename
from inferyard.evidence.storage import (
    EvidenceError,
    EvidenceStore,
    json_bytes,
    local_file,
    verify_manifest,
)
from inferyard.platforms.platform_io import filesystem_path, legacy_request_alias

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location(
    "snapshot_migration", ROOT / "scripts/migrate_request_snapshots.py"
)
module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(module)


def packet(root, original, *, physical=None, directory="packet"):
    path = root / "validation" / directory
    filesystem_path(path).mkdir(parents=True)
    run = json_bytes({"run_id": "run-old"})
    body = b'{"prompt":"unchanged bytes"}\n'
    filesystem_path(path / "run.json").write_bytes(run)
    files = {"run.json": run, original: body}
    manifest = json_bytes(
        {
            "schema_version": 3,
            "sealed": True,
            "run_id": "run-old",
            "files": {
                name: {
                    "bytes": len(content),
                    "sha256": hashlib.sha256(content).hexdigest(),
                    "derived": False,
                }
                for name, content in files.items()
            },
        }
    )
    filesystem_path(path / "manifest.json").write_bytes(manifest)
    if physical is not False:
        target = path / (physical or original)
        filesystem_path(target.parent).mkdir(parents=True, exist_ok=True)
        filesystem_path(target).write_bytes(body)
    return path, body, manifest


@pytest.mark.parametrize(
    "original",
    ["formal-12.request.json", "run-old-formal-12.request.json", "run-old:formal:12.request.json"],
)
def test_rename_preserves_manifest_hash_and_binds_original_key(tmp_path, original):
    alias = legacy_request_alias(original)
    path, body, manifest = packet(tmp_path, original, physical=alias)
    plan = module.build_plan(tmp_path, recover_git=False)
    result = module.apply_plan(plan, "backup", "migration.json")
    target = path / snapshot_filename("run-old", "formal", 12)
    assert result["summary"]["actions"] == {"rename": 1}
    assert result["status"] == "complete"
    assert target.read_bytes() == body
    assert (path / "manifest.json").read_bytes() == manifest
    assert not (path / (alias or original)).is_file()
    assert local_file(path, original) == target
    assert local_file(tmp_path, "validation/packet/" + (alias or original)) == target
    if ":" in original:
        with pytest.raises(EvidenceError, match="invalid_manifest"):
            verify_manifest(path)
    else:
        assert verify_manifest(path) == []
    second = module.build_plan(tmp_path, recover_git=False)
    assert module.public_plan(second)["summary"]["actions"] == {"unchanged": 1}
    receipt = (tmp_path / "migration.json").read_bytes()
    assert module.apply_plan(second, "backup", "migration.json") == result
    assert (tmp_path / "migration.json").read_bytes() == receipt
    target.write_bytes(b"tampered")
    reason = "invalid_manifest" if ":" in original else "original_evidence_hash_mismatch"
    with pytest.raises(EvidenceError, match=reason):
        verify_manifest(path)


def test_git_recovery_uses_raw_bytes_and_never_opens_a_colon_path(tmp_path, monkeypatch):
    original = "run-old:formal:12.request.json"
    path, body, manifest = packet(tmp_path, original, physical=False)
    name = "validation/packet/" + original
    monkeypatch.setattr(module, "git_requests", lambda root: ("revision", {name: "blob"}))
    monkeypatch.setattr(module, "git_contents", lambda root, entries: {name: body})
    result = module.apply_plan(module.build_plan(tmp_path), "backup", "migration.json")
    assert result["summary"]["actions"] == {"recover_git": 1}
    assert (path / "manifest.json").read_bytes() == manifest
    # A legacy colon key is resolvable for explicit migration, but not a valid
    # current manifest. Archival copying must not silently upgrade it.
    with pytest.raises(EvidenceError, match="invalid_manifest"):
        verify_manifest(path)
    # A second invocation recognizes the already recovered copy and keeps its bytes.
    assert len(module.build_plan(tmp_path)["rows"]) == 1


def test_collision_stops_the_whole_plan_before_any_removal(tmp_path):
    path, body, manifest = packet(tmp_path, "formal-12.request.json")
    (path / snapshot_filename("run-old", "formal", 12)).write_bytes(b"different")
    with pytest.raises(EvidenceError, match="migration_target_collision"):
        module.build_plan(tmp_path, recover_git=False)
    assert (path / "formal-12.request.json").read_bytes() == body
    assert (path / "manifest.json").read_bytes() == manifest
    assert not (tmp_path / "backup").exists()


def test_corrupt_source_is_never_silently_repaired(tmp_path):
    path, _, _ = packet(tmp_path, "formal-12.request.json")
    (path / "formal-12.request.json").write_bytes(b"corrupt")
    with pytest.raises(EvidenceError, match="migration_original_body_hash_mismatch"):
        module.build_plan(tmp_path, recover_git=False)
    assert not (path / snapshot_filename("run-old", "formal", 12)).exists()


@pytest.mark.parametrize(
    "relative", ["../outside", "/outside", "C:/outside", "payload:stream", "validation/../outside"]
)
def test_migration_rejects_destinations_outside_workspace(tmp_path, relative):
    with pytest.raises(EvidenceError, match="unsafe_migration_path"):
        module.safe_path(tmp_path, relative)


def test_reader_cannot_follow_a_migrated_symlink_outside_packet(tmp_path):
    path, _, _ = packet(tmp_path, "run-old-formal-12.request.json")
    (path / "run-old-formal-12.request.json").unlink()
    external = tmp_path / "outside.json"
    external.write_bytes(b"external")
    try:
        (path / snapshot_filename("run-old", "formal", 12)).symlink_to(external)
    except OSError:
        pytest.skip("host does not permit creating symlinks")
    with pytest.raises(EvidenceError, match="unsafe_evidence_symlink"):
        local_file(path, "run-old-formal-12.request.json")


def test_existing_snapshot_migrates_and_reads_beyond_windows_max_path(tmp_path):
    directory = "a" * 100 + "/" + "b" * 40
    path, body, manifest = packet(tmp_path, "formal-12.request.json", directory=directory)
    target = path / snapshot_filename("run-old", "formal", 12)
    assert len(str(target.absolute())) > 260
    plan = module.build_plan(tmp_path, recover_git=False)
    module.apply_plan(plan, "backup", "migration.json")
    assert filesystem_path(target).read_bytes() == body
    assert local_file(path, "formal-12.request.json") == filesystem_path(target)
    assert verify_manifest(path) == []
    assert filesystem_path(path / "manifest.json").read_bytes() == manifest


def test_new_snapshot_and_manifest_publish_in_a_long_run_directory(tmp_path):
    root = tmp_path / ("a" * 60) / ("b" * 60)
    store = EvidenceStore(root, run_id="run-long-" + "c" * 55)
    try:
        name = snapshot_filename(store.run_id, "probe", 1)
        assert len(str(store.path / name)) > 260
        store.snapshot("run.json", {"run_id": store.run_id})
        store.snapshot(name, {"prompt": "long path"})
        store.seal()
        assert local_file(store.path, name).is_file()
        assert verify_manifest(store.path) == []
    finally:
        store.close()
