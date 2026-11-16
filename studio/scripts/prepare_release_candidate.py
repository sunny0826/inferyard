"""Create a strict Studio release candidate from three verified single-target packages."""

import argparse
import json
from pathlib import Path

import release_common as common

RECEIPT_KEYS = {
    "kind", "version", "source_commit", "source_dirty", "target", "archive", "binary", "build_info",
}
MANIFEST_NAME = "studio-release-manifest.json"


def load_receipt(directory: Path) -> dict[str, common.JsonData]:
    receipts = sorted(Path(directory).glob("studio-package-*.json"))
    if len(receipts) != 1:
        raise ValueError("package_receipt_missing")
    data = common.read_json(receipts[0].read_bytes())
    if not isinstance(data, dict) or set(data) != RECEIPT_KEYS or data.get("kind") != common.PACKAGE_KIND:
        raise ValueError("package_receipt_invalid")
    return data


def checked_package(
    directory: Path, data: dict[str, common.JsonData], source_commit: str
) -> tuple[str, common.ApprovedFile, dict[str, common.JsonData], str]:
    target = data.get("target")
    if target not in common.TARGETS:
        raise ValueError("package_target_set")
    if data.get("source_commit") != source_commit or data.get("source_dirty") is not False:
        raise ValueError("package_source_mismatch")
    common.validate_build_info(data.get("build_info"), source_commit, target)
    archive = common.checked_file(directory, data["archive"])
    common.checked_file(directory, data["binary"])
    return target, archive, data["build_info"], data["version"]


def _checked_packages(
    inputs: list[Path], source_commit: str
) -> dict[str, tuple[common.ApprovedFile, dict[str, common.JsonData], str]]:
    """Validate every input against its receipt and bytes before anything is written."""
    by_target = {}
    for directory in inputs:
        directory = Path(directory)
        target, archive, info, version = checked_package(
            directory, load_receipt(directory), source_commit
        )
        if target in by_target:
            raise ValueError("duplicate_package_target")
        by_target[target] = (archive, info, version)
    if set(by_target) != set(common.TARGETS):
        raise ValueError("package_target_set")
    return by_target


def _consistent_version(
    by_target: dict[str, tuple[common.ApprovedFile, dict[str, common.JsonData], str]]
) -> str:
    shared = [
        {key: value for key, value in info.items() if key not in ("goos", "goarch")}
        for _, info, _ in by_target.values()
    ]
    versions = {version for _, _, version in by_target.values()}
    if len(versions) != 1 or any(entry != shared[0] for entry in shared[1:]):
        raise ValueError("package_receipt_inconsistent")
    version = versions.pop()
    if version != common.studio_version():
        raise ValueError("development_or_unfrozen_version")
    return version


def _check_archive_names(
    by_target: dict[str, tuple[common.ApprovedFile, dict[str, common.JsonData], str]], version: str
) -> None:
    for target in common.TARGETS:
        if by_target[target][0].name != common.archive_name(version, target):
            raise ValueError("package_archive_name")


def _write_candidate(
    out: Path,
    by_target: dict[str, tuple[common.ApprovedFile, dict[str, common.JsonData], str]],
    version: str,
    source_commit: str,
) -> dict[str, common.JsonData]:
    out.mkdir()
    artifacts = []
    for target in common.TARGETS:
        archive, info, _ = by_target[target]
        target_path = out / archive.name
        with target_path.open("xb") as stream:
            stream.write(archive.raw)
        if common.sha(target_path.read_bytes()) != archive.digest:
            raise ValueError("staged_bytes_changed")
        artifacts.append({
            "target": target,
            "path": archive.name,
            "bytes": len(archive.raw),
            "sha256": archive.digest,
            "build_info": info,
        })
    manifest = {
        "kind": common.MANIFEST_KIND,
        "version": version,
        "source_commit": source_commit,
        "source_dirty": False,
        "artifacts": artifacts,
    }
    with (out / MANIFEST_NAME).open("x", encoding="utf-8") as stream:
        json.dump(manifest, stream, indent=2)
        stream.write("\n")
    manifest_raw = (out / MANIFEST_NAME).read_bytes()
    checksums = "".join(f"{artifact['sha256']}  {artifact['path']}\n" for artifact in artifacts)
    with (out / common.CHECKSUMS_NAME).open("xb") as stream:
        stream.write(checksums.encode("utf-8"))
    return {
        "manifest": str(out / MANIFEST_NAME),
        "manifest_sha256": common.sha(manifest_raw),
        "source_commit": source_commit,
        "version": version,
    }


def prepare(inputs: list[Path], out: Path, source_commit: str) -> dict[str, common.JsonData]:
    if len(inputs) != len(common.TARGETS):
        raise ValueError("package_input_count")
    common.require_source_commit(source_commit)
    by_target = _checked_packages(inputs, source_commit)
    version = _consistent_version(by_target)
    _check_archive_names(by_target, version)
    return _write_candidate(out, by_target, version, source_commit)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--input", type=Path, action="append", required=True,
        help="single-target package directory; repeat exactly three times",
    )
    parser.add_argument("--source-commit", required=True)
    parser.add_argument("--out", type=Path, required=True, help="new candidate directory")
    args = parser.parse_args()
    print(json.dumps(prepare(args.input, args.out, args.source_commit)))


if __name__ == "__main__":
    main()
