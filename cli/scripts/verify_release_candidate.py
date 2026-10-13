"""Verify v2 installation coverage and stage the same checked bytes; never build or upload."""

import argparse
import json
from pathlib import Path

if __package__:
    from .release_common import (
        TARGETS,
        artifact_files,
        checked_file,
        read_json,
        relative_path,
        sha,
        source_identity,
        validate_installation,
    )
    from .release_validation import validate_build
else:
    from release_common import (
        TARGETS,
        artifact_files,
        checked_file,
        read_json,
        relative_path,
        sha,
        source_identity,
        validate_installation,
    )
    from release_validation import validate_build


def verify(manifest, expected_sha, expected_commit):
    raw = manifest.read_bytes()
    if sha(raw) != expected_sha:
        raise ValueError("acceptance_manifest_hash_mismatch")
    data = read_json(raw)
    if (
        data.get("kind") != "community_distribution.v2"
        or data.get("candidate_status") != "release_candidate"
    ):
        raise ValueError("candidate_not_approved_v2")
    source_identity(data, expected_commit)
    files = artifact_files(manifest.parent, data, checked_file)
    build_file = checked_file(manifest.parent, data["build_manifest"])
    build = read_json(build_file.raw)
    # Bind original descriptors to the snapshots already read above; never reopen artifacts.
    seen = set()
    for record in build.get("artifacts", []):
        role = record.get("role")
        if role not in files or role in seen:
            raise ValueError("candidate_build_artifact_mismatch")
        seen.add(role)
        file = files[role]
        if (
            build_file.path.parent / relative_path(record.get("path")) != file.path
            or record.get("sha256") != file.digest
            or type(record.get("bytes")) is not int
            or record["bytes"] != len(file.raw)
        ):
            raise ValueError("candidate_build_artifact_mismatch")
    if seen != set(files):
        raise ValueError("candidate_build_artifact_mismatch")
    validate_build(build, files, expected_commit)
    results = data.get("installation_results")
    if not isinstance(results, list) or not results:
        raise ValueError("installation_evidence_missing")
    coverage = dict.fromkeys(sorted(TARGETS), "not_verified")
    for record in results:
        evidence = read_json(checked_file(manifest.parent, record).raw)
        target = validate_installation(evidence, build, build_file.digest, files)
        if coverage[target] == "passed":
            raise ValueError("duplicate_installation_platform")
        coverage[target] = "passed"
    if data.get("platform_coverage") != coverage:
        raise ValueError("installation_coverage_mismatch")
    return files


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--manifest-sha256", required=True)
    parser.add_argument("--source-commit", required=True)
    parser.add_argument("--stage", type=Path, required=True, help="new staging directory")
    parser.add_argument("--packages-only", action="store_true", help="stage wheel/sdist only")
    args = parser.parse_args()
    files = verify(args.manifest, args.manifest_sha256, args.source_commit)
    args.stage.mkdir()
    roles = ("wheel", "sdist") if args.packages_only else tuple(sorted(files))
    for role in roles:
        target = args.stage / files[role].name
        with target.open("xb") as stream:
            stream.write(files[role].raw)
        if sha(target.read_bytes()) != files[role].digest:
            raise ValueError("staged_bytes_changed")
    print(
        json.dumps(
            {
                "verified": True,
                "upload_performed": False,
                "files": [str(args.stage / files[r].name) for r in roles],
            }
        )
    )


if __name__ == "__main__":
    main()
