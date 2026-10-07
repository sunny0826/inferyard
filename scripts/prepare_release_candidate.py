"""Create a portable v2 candidate from checked build bytes and completed installation results."""

import argparse
import json
from pathlib import Path

if __package__:
    from .release_common import (
        TARGETS,
        artifact_files,
        file_record,
        read_json,
        sha,
        validate_installation,
    )
    from .release_validation import validate_build
else:
    from release_common import (
        TARGETS,
        artifact_files,
        file_record,
        read_json,
        sha,
        validate_installation,
    )
    from release_validation import validate_build


def prepare(manifest, installations, out, source_commit):
    build_raw = manifest.read_bytes()
    build = read_json(build_raw)
    files = artifact_files(manifest.parent, build)
    validate_build(build, files, source_commit)
    evidence = []
    coverage = dict.fromkeys(sorted(TARGETS), "not_verified")
    for path in installations:
        raw = path.read_bytes()
        target = validate_installation(read_json(raw), build, sha(build_raw), files)
        if coverage[target] == "passed":
            raise ValueError("duplicate_installation_platform")
        coverage[target] = "passed"
        evidence.append((target, raw))
    if not evidence:
        raise ValueError("installation_evidence_missing")
    # Complete validation before creating output. Every subsequent write uses these snapshots.
    out.mkdir()

    def write(name, raw):
        target = out / name
        target.parent.mkdir(parents=True, exist_ok=True)
        with target.open("xb") as stream:
            stream.write(raw)
        return file_record(name, raw)

    write("build/manifest.json", build_raw)
    for file in files.values():
        write("build/" + file.path.as_posix(), file.raw)
    result = {
        "kind": "community_distribution.v2",
        "candidate_status": "release_candidate",
        "version": build["version"],
        "source_commit": source_commit,
        "source_dirty": False,
        "build_manifest": file_record("build/manifest.json", build_raw),
        "artifacts": [
            {"role": role, **file_record("build/" + file.path.as_posix(), file.raw)}
            for role, file in files.items()
        ],
        "installation_results": [
            write(f"installations/{target}.json", raw) for target, raw in evidence
        ],
        "platform_coverage": coverage,
        "qualification": "installation_only_not_native_model_or_performance",
    }
    raw = (json.dumps(result, indent=2) + "\n").encode("utf-8")
    write("manifest.json", raw)
    return {
        "manifest": str(out / "manifest.json"),
        "manifest_sha256": sha(raw),
        "source_commit": source_commit,
        "platform_coverage": coverage,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True, help="original build manifest")
    parser.add_argument(
        "--installed",
        type=Path,
        action="append",
        required=True,
        help="completed result.json; repeat for each tested platform",
    )
    parser.add_argument("--source-commit", required=True)
    parser.add_argument("--out", type=Path, required=True, help="new portable candidate directory")
    args = parser.parse_args()
    print(json.dumps(prepare(args.manifest, args.installed, args.out, args.source_commit)))


if __name__ == "__main__":
    main()
