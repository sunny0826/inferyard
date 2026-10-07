"""Rename existing request bodies, preserving sealed bytes and historical manifest keys.

The default is a read-only plan. --apply publishes verified copies before removing
old regular files. Missing Windows colon names can be recovered from Git HEAD.
Backups and an idempotent migration receipt stay under the chosen repository.
"""

import argparse
import hashlib
import json
import os
import re
import subprocess
from collections import Counter
from pathlib import Path, PurePosixPath, PureWindowsPath

from inferyard.evidence.request_snapshots import legacy_snapshot_filename, snapshot_filename
from inferyard.evidence.storage import (
    EvidenceError,
    atomic_bytes,
    json_bytes,
    read_json,
    sha256_file,
)
from inferyard.platforms.platform_io import (
    filesystem_path,
    legacy_request_alias,
    open_nofollow,
    resolved_within,
)

SCOPES = ("validation", "results", "reports", "tests/fixtures")
CANONICAL = re.compile(r"(.+)__([0-9]{6,})__(probe|warmup|formal)\.request\.json")


def safe_path(root, relative, *, allow_legacy=False):
    """Check the absolute workspace boundary and reject ADS/reparse points."""
    name = PurePosixPath(relative)
    legacy_colon = (
        allow_legacy
        and os.name != "nt"
        and ":" in name.name
        and legacy_snapshot_filename(name.name) is not None
        and all(":" not in p for p in name.parts[:-1])
    )
    if (
        name.is_absolute()
        or (PureWindowsPath(relative).drive and not legacy_colon)
        or ".." in name.parts
        or not name.parts
        or any("\\" in p for p in name.parts)
        or (any(":" in p for p in name.parts) and not legacy_colon)
    ):
        raise EvidenceError("unsafe_migration_path")
    path = root.joinpath(*name.parts)
    if not resolved_within(path, root):
        raise EvidenceError("unsafe_migration_path")
    for part in (path, *path.parents):
        if part == root:
            break
        part = filesystem_path(part)
        if part.exists() or part.is_symlink():
            if part.is_symlink() or getattr(part.lstat(), "st_file_attributes", 0) & 0x400:
                raise EvidenceError("migration_reparse_point")
    return filesystem_path(path)


def physical_requests(root):
    paths = []
    for scope in SCOPES:
        base = safe_path(root, scope)
        if not base.exists():
            continue
        for directory, directories, files in os.walk(base, followlinks=False):
            for name in directories:
                safe_path(root, (Path(directory) / name).relative_to(root).as_posix())
            for name in files:
                if name.endswith(".request.json"):
                    paths.append((Path(directory) / name).relative_to(root).as_posix())
    return sorted(paths)


def git_requests(root):
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root).decode().strip()
    tree = subprocess.check_output(
        ["git", "ls-tree", "-r", "-z", revision, "--", *SCOPES], cwd=root
    )
    entries = {}
    for row in tree.split(b"\0"):
        if not row:
            continue
        header, filename = row.split(b"\t", 1)
        filename = filename.decode("utf-8")
        if filename.endswith(".request.json"):
            mode, kind, digest = header.decode().split()
            if mode not in ("100644", "100755") or kind != "blob":
                raise EvidenceError("unsupported_git_request_entry")
            entries[filename] = digest
    return revision, entries


def git_contents(root, entries):
    if not entries:
        return {}
    process = subprocess.Popen(
        ["git", "cat-file", "--batch"], cwd=root, stdin=subprocess.PIPE, stdout=subprocess.PIPE
    )
    result = {}
    try:
        for name, blob in sorted(entries.items()):
            process.stdin.write((blob + "\n").encode("ascii"))
            process.stdin.flush()
            header = process.stdout.readline().decode().split()
            if len(header) != 3 or header[:2] != [blob, "blob"]:
                raise EvidenceError("invalid_git_request_blob")
            size = int(header[2])
            if size > 16 * 1024 * 1024:
                raise EvidenceError("request_snapshot_exceeds_migration_limit")
            content = process.stdout.read(size)
            if len(content) != size or process.stdout.read(1) != b"\n":
                raise EvidenceError("invalid_git_request_blob")
            result[name] = content
    finally:
        process.stdin.close()
        if process.wait(timeout=10):
            raise EvidenceError("git_request_read_failed")
    return result


def read_body(path):
    with os.fdopen(open_nofollow(path, os.O_RDONLY), "rb") as stream:
        content = stream.read(16 * 1024 * 1024 + 1)
    if len(content) > 16 * 1024 * 1024:
        raise EvidenceError("request_snapshot_exceeds_migration_limit")
    return content


def build_plan(root, *, recover_git=True):
    root = root.resolve()
    revision, tracked = git_requests(root) if recover_git else (None, {})
    committed = git_contents(root, tracked)
    physical = set(physical_requests(root))
    packets, rows, targets = {}, [], {}
    for original in sorted(physical | set(tracked)):
        path = PurePosixPath(original)
        packet = path.parent
        logical = path.name
        if packet.name == "request-files":
            packet = packet.parent
            manifest = read_json(safe_path(root, (packet / "manifest.json").as_posix()))
            matches = [
                n
                for n in manifest["files"]
                if legacy_request_alias(n) == "request-files/" + logical
            ]
            if len(matches) != 1:
                raise EvidenceError("unbound_archival_request_alias")
            logical = matches[0]
        key = packet.as_posix()
        if key not in packets:
            run = read_json(safe_path(root, (packet / "run.json").as_posix()))
            manifest_path = safe_path(root, (packet / "manifest.json").as_posix())
            packets[key] = {
                "run_id": run["run_id"],
                "manifest": read_json(manifest_path) if manifest_path.exists() else None,
                "manifest_sha256": sha256_file(manifest_path) if manifest_path.exists() else None,
            }
        metadata = packets[key]
        mapped = legacy_snapshot_filename(logical, metadata["run_id"])
        if mapped is None:
            match = CANONICAL.fullmatch(logical)
            if not match or snapshot_filename(match[1], match[3], int(match[2])) != logical:
                raise EvidenceError("unknown_request_snapshot_filename")
            mapped = logical
        # The physical file and its packet must agree on the run identity.
        if not mapped.startswith(metadata["run_id"] + "__"):
            raise EvidenceError("migration_run_identity_mismatch")
        target = (packet / mapped).as_posix()
        target_path = safe_path(root, target)
        source_path = safe_path(root, original, allow_legacy=True) if original in physical else None
        content = read_body(source_path) if source_path else committed[original]
        digest = hashlib.sha256(content).hexdigest()
        if original in committed and content != committed[original]:
            raise EvidenceError("migration_worktree_body_differs_from_git")
        manifest = metadata["manifest"]
        if manifest is not None:
            if logical not in manifest.get("files", {}) and original == target:
                matches = [
                    n
                    for n in manifest["files"]
                    if legacy_snapshot_filename(n, metadata["run_id"]) == mapped
                ]
                if len(matches) == 1:
                    logical = matches[0]
            info = manifest.get("files", {}).get(logical)
            if info is None or info.get("sha256") != digest or info.get("bytes") != len(content):
                raise EvidenceError("migration_original_body_hash_mismatch")
        if target_path.exists() and read_body(target_path) != content:
            raise EvidenceError("migration_target_collision")
        row = {
            "packet": key,
            "original": original,
            "manifest_name": logical,
            "target": target,
            "sha256": digest,
            "bytes": len(content),
            "git_blob": tracked.get(original),
            "action": "unchanged"
            if original == target
            else "rename"
            if source_path
            else "recover_git",
            "_content": content,
        }
        if target in targets:
            previous = targets[target]
            if previous["sha256"] != digest:
                raise EvidenceError("migration_target_collision")
            # An idempotent rerun sees both the historical Git entry and its
            # already published canonical working file; retain the historical mapping.
            if row["action"] == "unchanged":
                continue
            if previous["action"] != "unchanged":
                raise EvidenceError("migration_duplicate_identity")
            rows.remove(previous)
        targets[target] = row
        rows.append(row)
    return {"root": root, "git_revision": revision, "packets": packets, "rows": rows}


def public_plan(plan):
    return {
        "kind": "request_snapshot_migration.v1",
        "filename_format": "{run_id}__{ordinal:06d}__{phase}.request.json",
        "git_revision": plan["git_revision"],
        "summary": {
            "bodies": len(plan["rows"]),
            "packets": len(plan["packets"]),
            "actions": dict(Counter(r["action"] for r in plan["rows"])),
        },
        "packets": {
            k: {n: v for n, v in p.items() if n != "manifest"} for k, p in plan["packets"].items()
        },
        "files": [{k: v for k, v in r.items() if k != "_content"} for r in plan["rows"]],
        "original_body_bytes_preserved": True,
        "original_manifest_bytes_preserved": True,
        "hardware_qualification_added": False,
        "model_requests": 0,
    }


def apply_plan(plan, backup, receipt):
    root = plan["root"]
    # Verify both destinations before any write or removal.
    backup = safe_path(root, backup)
    receipt_path = safe_path(root, receipt)
    if any(resolved_within(receipt_path, root / packet) for packet in plan["packets"]):
        raise EvidenceError("migration_receipt_inside_original_packet")
    if any(resolved_within(backup, root / packet) for packet in plan["packets"]):
        raise EvidenceError("migration_backup_inside_original_packet")
    previous = None
    if receipt_path.exists():
        previous = read_json(receipt_path)
        if previous.get("kind") != "request_snapshot_migration.v1":
            raise EvidenceError("migration_receipt_collision")
        signature = {(r["target"], r["sha256"], r["bytes"]) for r in plan["rows"]}
        previous_signature = {
            (r["target"], r["sha256"], r["bytes"]) for r in previous.get("files", [])
        }
        if previous.get("status") in ("complete", "in_progress"):
            manifests = {k: p["manifest_sha256"] for k, p in plan["packets"].items()}
            old_manifests = {k: p["manifest_sha256"] for k, p in previous["packets"].items()}
            if signature != previous_signature or manifests != old_manifests:
                raise EvidenceError("completed_migration_receipt_collision")
            if previous["status"] == "complete" and set(physical_requests(root)) == {
                r["target"] for r in plan["rows"]
            }:
                # Retain the first receipt's original rename/recovery history.
                return previous
    backup.mkdir(parents=True, exist_ok=True)
    receipt_path.parent.mkdir(parents=True, exist_ok=True)
    value = previous or public_plan(plan)
    value["status"] = "in_progress"
    atomic_bytes(receipt_path, json_bytes(value), overwrite=True)
    for row in plan["rows"]:
        target = safe_path(root, row["target"])
        if row["action"] != "unchanged":
            saved = safe_path(
                root, (backup / (row["sha256"] + ".json")).relative_to(root).as_posix()
            )
            if not saved.exists():
                atomic_bytes(saved, row["_content"])
            if sha256_file(saved) != row["sha256"]:
                raise EvidenceError("migration_backup_hash_mismatch")
        if not target.exists():
            target.parent.mkdir(parents=True, exist_ok=True)
            atomic_bytes(target, row["_content"])
        if target.stat().st_size != row["bytes"] or sha256_file(target) != row["sha256"]:
            raise EvidenceError("migration_published_hash_mismatch")
        if row["action"] == "rename":
            original = safe_path(root, row["original"], allow_legacy=True)
            if read_body(original) != row["_content"]:
                raise EvidenceError("migration_original_changed_during_apply")
            original.unlink()
    for key, metadata in plan["packets"].items():
        manifest_path = safe_path(root, key + "/manifest.json")
        current = sha256_file(manifest_path) if manifest_path.exists() else None
        if current != metadata["manifest_sha256"]:
            raise EvidenceError("migration_manifest_changed")
    actual = set(physical_requests(root))
    expected = {row["target"] for row in plan["rows"]}
    if actual != expected:
        raise EvidenceError("migration_inventory_mismatch")
    value["status"] = "complete"
    value["backup_directory"] = backup.relative_to(root).as_posix()
    atomic_bytes(receipt_path, json_bytes(value), overwrite=True)
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--no-git-recovery", action="store_true")
    parser.add_argument("--backup", default=".tools/request-migration-backup")
    parser.add_argument(
        "--receipt", default="validation/windows/request-snapshot-migration-20261001/migration.json"
    )
    args = parser.parse_args()
    plan = build_plan(args.root, recover_git=not args.no_git_recovery)
    value = apply_plan(plan, args.backup, args.receipt) if args.apply else public_plan(plan)
    print(json.dumps({"status": value.get("status", "planned"), **value["summary"]}))


if __name__ == "__main__":
    main()
