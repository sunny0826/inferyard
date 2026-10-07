"""Copy a sealed Git evidence packet to portable names without changing its bytes or manifest."""

import argparse
import json
import subprocess
from pathlib import Path, PurePosixPath

from inferyard.evidence.migration_source import load_source
from inferyard.evidence.request_snapshots import legacy_snapshot_filename
from inferyard.evidence.storage import EvidenceError, atomic_bytes, json_bytes, local_file
from inferyard.platforms.platform_io import legacy_request_alias


def copy_packet(git_path, out, *, repository=None):
    repository = repository or Path(__file__).resolve().parents[1]
    name = PurePosixPath(git_path)
    if name.is_absolute() or ".." in name.parts or not name.parts or name.parts[0] != "validation":
        raise EvidenceError("requires_committed_validation_packet")
    origin = name.as_posix().rstrip("/") + "/"
    revision = (
        subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=repository).decode().strip()
    )
    tree = subprocess.check_output(
        ["git", "ls-tree", "-r", "-z", revision, "--", origin], cwd=repository
    )
    run = json.loads(
        subprocess.check_output(
            ["git", "show", revision + ":" + origin + "run.json"], cwd=repository
        )
    )
    entries = []
    for row in tree.split(b"\0"):
        if not row:
            continue
        header, filename = row.split(b"\t", 1)
        mode, kind, digest = header.decode().split()
        filename = filename.decode("utf-8")
        if mode not in ("100644", "100755") or kind != "blob" or not filename.startswith(origin):
            raise EvidenceError("unsupported_git_evidence_entry")
        relative = filename[len(origin) :]
        alias = legacy_request_alias(relative)
        # Validate every target before creating the output; reject arbitrary ':'
        # paths instead of treating them as Windows alternate streams.
        portable = legacy_snapshot_filename(relative, run["run_id"])
        target = portable or alias or relative
        local_file(out, target)
        entries.append((relative, target, digest))
    if "manifest.json" not in {entry[0] for entry in entries}:
        raise EvidenceError("requires_sealed_git_packet")
    if "windows-portability.json" in {entry[0] for entry in entries}:
        raise EvidenceError("portability_receipt_collision")
    out.mkdir(parents=True, exist_ok=False)
    process = subprocess.Popen(
        ["git", "cat-file", "--batch"],
        cwd=repository,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
    )
    aliases = {}
    try:
        for relative, target, digest in entries:
            process.stdin.write((digest + "\n").encode("ascii"))
            process.stdin.flush()
            header = process.stdout.readline().decode().split()
            if len(header) != 3 or header[:2] != [digest, "blob"]:
                raise EvidenceError("invalid_git_evidence_blob")
            content = process.stdout.read(int(header[2]))
            if process.stdout.read(1) != b"\n":
                raise EvidenceError("invalid_git_evidence_delimiter")
            path = local_file(out, target)
            path.parent.mkdir(parents=True, exist_ok=True)
            atomic_bytes(path, content)
            if relative != target:
                aliases[relative] = target
    finally:
        process.stdin.close()
        process.wait(timeout=10)
    # This copies legacy bytes; only the explicit migration reader accepts them.
    load_source(out)
    receipt = {
        "kind": "windows_evidence_copy.v1",
        "git_revision": revision,
        "git_path": name.as_posix(),
        "file_count": len(entries),
        "aliases": aliases,
        "original_bytes_and_manifest_preserved": True,
        "hardware_qualification_added": False,
        "manifest_limitations": [],
        "next_step": "inferyard migrate --run <portable-copy> --out <new-current-run>",
    }
    atomic_bytes(out / "windows-portability.json", json_bytes(receipt))
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--git-path", required=True, help="committed validation packet directory")
    parser.add_argument("--out", type=Path, required=True, help="new Windows evidence directory")
    args = parser.parse_args()
    receipt = copy_packet(args.git_path, args.out.resolve())
    print(json.dumps({"out": str(args.out), **receipt}, ensure_ascii=False))


if __name__ == "__main__":
    main()
