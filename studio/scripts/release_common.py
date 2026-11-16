"""Shared constants, byte checks and source identity for Studio release packaging."""

import hashlib
import json
import re
from dataclasses import dataclass
from pathlib import Path, PurePosixPath

TARGETS = ("darwin-arm64", "linux-amd64", "windows-amd64")
GOOS_GOARCH = {
    "darwin-arm64": ("darwin", "arm64"),
    "linux-amd64": ("linux", "amd64"),
    "windows-amd64": ("windows", "amd64"),
}
STUDIO_ROOT = Path(__file__).resolve().parents[1]
PACKAGE_KIND = "studio-package.v1"
MANIFEST_KIND = "studio-release-candidate.v1"
CHECKSUMS_NAME = "SHA256SUMS.studio"
ARTIFACT_KEYS = {"target", "path", "bytes", "sha256", "build_info"}
BUILD_INFO_KEYS = {"go_version", "goos", "goarch", "tags", "vcs_revision", "vcs_modified", "vcs_time"}
HEX_DIGEST = re.compile(r"[0-9a-f]{64}")
COMMIT = re.compile(r"[0-9a-f]{40}")
JsonData = str | int | float | bool | None | list["JsonData"] | dict[str, "JsonData"]


def studio_version() -> str:
    """Read the single version source (mygo.config.ts) of this checkout."""
    text = (STUDIO_ROOT / "mygo.config.ts").read_text(encoding="utf-8")
    match = re.search(r"^\s*version:\s*['\"]([^'\"]+)['\"]", text, re.M)
    if not match:
        raise ValueError("studio_version_missing")
    return match.group(1)


def executable_name(target: str) -> str:
    return "inferyard-studio.exe" if target.startswith("windows") else "inferyard-studio"


def distribution_name(version: str, target: str) -> str:
    return f"inferyard-studio-v{version}-{target}"


def archive_name(version: str, target: str) -> str:
    suffix = ".zip" if target.startswith("windows") else ".tar.gz"
    return f"{distribution_name(version, target)}{suffix}"


def reject_duplicates(pairs: list[tuple[str, JsonData]]) -> dict[str, JsonData]:
    result: dict[str, JsonData] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate_manifest_key")
        result[key] = value
    return result


def read_json(raw: bytes) -> dict[str, JsonData]:
    return json.loads(
        raw,
        object_pairs_hook=reject_duplicates,
        parse_constant=lambda value: (_ for _ in ()).throw(ValueError(value)),
    )


def sha(raw: bytes) -> str:
    return hashlib.sha256(raw).hexdigest()


def relative_path(name: JsonData) -> PurePosixPath:
    if not isinstance(name, str) or not name or name == ".":
        raise ValueError("unsafe_candidate_path")
    path = PurePosixPath(name)
    if (
        "\\" in name
        or ":" in name
        or path.is_absolute()
        or ".." in path.parts
        or path.as_posix() != name
    ):
        raise ValueError("unsafe_candidate_path")
    return path


@dataclass(frozen=True)
class ApprovedFile:
    path: PurePosixPath
    raw: bytes
    digest: str

    @property
    def name(self) -> str:
        return self.path.name


def checked_file(root: Path, record: dict[str, JsonData]) -> ApprovedFile:
    """Bind a descriptor to the actual bytes under root; reject links and mismatches."""
    path = relative_path(record.get("path"))
    target = root.joinpath(*path.parts)
    # Allow symlinked ancestors such as macOS /tmp; reject links at or below the bundle root.
    if root.is_symlink() or any(
        p.is_symlink() for p in [target, *target.parents] if p == root or p.is_relative_to(root)
    ):
        raise ValueError("candidate_symlink")
    try:
        raw = target.read_bytes()
    except OSError as error:
        raise ValueError("candidate_file_missing") from error
    if (
        type(record.get("bytes")) is not int
        or record["bytes"] != len(raw)
        or record.get("sha256") != sha(raw)
    ):
        raise ValueError("candidate_hash_mismatch")
    return ApprovedFile(path, raw, record["sha256"])


def file_record(name: str, raw: bytes) -> dict[str, JsonData]:
    return {"path": str(name), "bytes": len(raw), "sha256": sha(raw)}


def require_source_commit(commit: JsonData) -> None:
    if not isinstance(commit, str) or COMMIT.fullmatch(commit) is None:
        raise ValueError("source_commit_invalid")


def _shape_mismatch(info: dict[str, JsonData]) -> bool:
    return (
        not isinstance(info, dict)
        or set(info) != BUILD_INFO_KEYS
        or not isinstance(info["go_version"], str)
        or not isinstance(info["vcs_time"], str)
    )


def _tags_invalid(tags: JsonData) -> bool:
    return (
        not isinstance(tags, list)
        or any(type(tag) is not str for tag in tags)
        or "release" not in tags
    )


def validate_build_info(info: dict[str, JsonData], commit: str, target: str) -> None:
    """Require release tags and clean VCS evidence bound to the target platform."""
    if _shape_mismatch(info):
        raise ValueError("build_info_invalid")
    if (info["goos"], info["goarch"]) != GOOS_GOARCH[target]:
        raise ValueError("build_info_target_mismatch")
    if _tags_invalid(info["tags"]):
        raise ValueError("build_info_not_release")
    if info["vcs_modified"] is not False or info["vcs_revision"] != commit:
        raise ValueError("build_info_vcs_mismatch")


def check_source_identity(data: dict[str, JsonData], commit: str) -> None:
    require_source_commit(commit)
    if data.get("source_commit") != commit:
        raise ValueError("candidate_commit_mismatch")
    if data.get("source_dirty") is not False:
        raise ValueError("candidate_source_dirty")
