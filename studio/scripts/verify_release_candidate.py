"""Verify a Studio release candidate and stage the same checked bytes; never repackage."""

import argparse
import json
import re
from pathlib import Path

import release_common as common

MANIFEST_KEYS = {"kind", "version", "source_commit", "source_dirty", "artifacts"}
CHECKSUM_LINE = re.compile(r"([0-9a-f]{64})  (\S+)")


def _manifest_data(manifest: Path, expected_sha: str, source_commit: str) -> dict[str, common.JsonData]:
    raw = manifest.read_bytes()
    if (
        not isinstance(expected_sha, str)
        or common.HEX_DIGEST.fullmatch(expected_sha) is None
        or common.sha(raw) != expected_sha
    ):
        raise ValueError("manifest_hash_mismatch")
    data = common.read_json(raw)
    if (
        not isinstance(data, dict)
        or set(data) != MANIFEST_KEYS
        or data.get("kind") != common.MANIFEST_KIND
    ):
        raise ValueError("candidate_not_studio_release")
    if data.get("version") != common.studio_version():
        raise ValueError("development_or_unfrozen_version")
    common.check_source_identity(data, source_commit)
    return data


def _artifact_record(record: common.JsonData) -> dict[str, common.JsonData]:
    if not isinstance(record, dict) or set(record) != common.ARTIFACT_KEYS:
        raise ValueError("candidate_artifact_set")
    return record


def _checked_artifacts(
    data: dict[str, common.JsonData], root: Path, source_commit: str
) -> dict[str, common.ApprovedFile]:
    artifacts = data.get("artifacts")
    if not isinstance(artifacts, list) or len(artifacts) != len(common.TARGETS):
        raise ValueError("candidate_artifact_set")
    by_target = {}
    for record in artifacts:
        record = _artifact_record(record)
        target = record["target"]
        if target not in common.TARGETS or target in by_target:
            raise ValueError("candidate_artifact_set")
        common.validate_build_info(record["build_info"], source_commit, target)
        file = common.checked_file(root, record)
        if file.name != common.archive_name(data["version"], target):
            raise ValueError("candidate_artifact_name")
        by_target[target] = file
    if len({file.name for file in by_target.values()}) != len(common.TARGETS):
        raise ValueError("candidate_artifact_paths")
    return by_target


def _read_checksums(root: Path) -> bytes:
    checksums_path = root / common.CHECKSUMS_NAME
    if checksums_path.is_symlink():
        raise ValueError("candidate_symlink")
    try:
        return checksums_path.read_bytes()
    except OSError as error:
        raise ValueError("checksums_missing") from error


def _ordered_archives(
    by_target: dict[str, common.ApprovedFile], checksums_raw: bytes
) -> list[common.ApprovedFile]:
    text = checksums_raw.decode("utf-8")
    lines = text.split("\n")
    if not text.endswith("\n") or lines[-1] != "":
        raise ValueError("checksums_invalid")
    lines.pop()
    if len(lines) != len(common.TARGETS):
        raise ValueError("checksums_invalid")
    ordered = []
    for target, line in zip(common.TARGETS, lines):
        match = CHECKSUM_LINE.fullmatch(line)
        file = by_target[target]
        if match is None or match.group(1) != file.digest or match.group(2) != file.name:
            raise ValueError("checksums_mismatch")
        ordered.append(file)
    return ordered


def verify(
    manifest: Path, expected_sha: str, source_commit: str
) -> tuple[list[common.ApprovedFile], bytes]:
    """Check the manifest and its bytes, returning ordered archives and the checksums file."""
    manifest = Path(manifest)
    data = _manifest_data(manifest, expected_sha, source_commit)
    by_target = _checked_artifacts(data, manifest.parent, source_commit)
    checksums_raw = _read_checksums(manifest.parent)
    return _ordered_archives(by_target, checksums_raw), checksums_raw


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--manifest-sha256", required=True)
    parser.add_argument("--source-commit", required=True)
    parser.add_argument("--stage", type=Path, required=True, help="new staging directory")
    args = parser.parse_args()
    ordered, checksums_raw = verify(args.manifest, args.manifest_sha256, args.source_commit)
    args.stage.mkdir()
    for file in ordered:
        with (args.stage / file.name).open("xb") as stream:
            stream.write(file.raw)
        if common.sha((args.stage / file.name).read_bytes()) != file.digest:
            raise ValueError("staged_bytes_changed")
    with (args.stage / common.CHECKSUMS_NAME).open("xb") as stream:
        stream.write(checksums_raw)
    print(json.dumps({
        "verified": True,
        "upload_performed": False,
        "files": [str(args.stage / file.name) for file in ordered]
        + [str(args.stage / common.CHECKSUMS_NAME)],
    }))


if __name__ == "__main__":
    main()
