"""Shared constants, byte checks and source identity for Studio release packaging."""

import hashlib
import re
from pathlib import Path

TARGETS = ("darwin-arm64", "linux-amd64", "windows-amd64")
GOOS_GOARCH = {
    "darwin-arm64": ("darwin", "arm64"),
    "linux-amd64": ("linux", "amd64"),
    "windows-amd64": ("windows", "amd64"),
}
STUDIO_ROOT = Path(__file__).resolve().parents[1]
PACKAGE_KIND = "studio-package.v1"
BUILD_INFO_KEYS = {
    "go_version",
    "goos",
    "goarch",
    "tags",
    "vcs_revision",
    "vcs_modified",
    "vcs_time",
}
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
    suffixes = {"darwin-arm64": ".dmg", "linux-amd64": ".tar.gz", "windows-amd64": ".exe"}
    return f"{distribution_name(version, target)}{suffixes[target]}"


def sha(raw: bytes) -> str:
    return hashlib.sha256(raw).hexdigest()


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
