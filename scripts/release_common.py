"""Byte snapshots and installation scope shared by release producers and consumers."""

import hashlib
import json
import re
from dataclasses import dataclass
from pathlib import PurePosixPath

TARGETS = {"linux-x64", "windows-x64", "macos-arm64"}
ROLES = {"wheel", "sdist", "constraints", "constraints_validation"}
REPOSITORY = "https://github.com/sunny0826/inferyard"
URLS = {"Repository": REPOSITORY, "Issues": REPOSITORY + "/issues"}
COMMANDS = (
    "install",
    "help",
    "versions",
    "schema",
    "catalogue",
    "engines",
    "init",
    "probe-install-a",
    "report",
    "verify",
    "verify-rerender",
    "tool-run",
    "uvx-hot-offline",
    "uvx-cold-offline",
    "uvx-cold-online-rebuild",
    "install-different-dependency",
    "different-versions",
    "uvx-isolates-existing-tool",
    "persistent-tool-unchanged",
    "install-b",
    "probe-install-b",
    "synthetic-upgrade",
    "synthetic-upgrade-versions",
    "rollback",
    "rollback-versions",
    "probe-rollback",
)
SCOPE = [
    "offline_core",
    "workspace_init",
    "report_verify",
    "resources_identity",
    "uv_uvx_isolation",
    "synthetic_upgrade_rollback",
    "synthetic_historical_reports",
]


def reject_duplicates(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate_manifest_key")
        result[key] = value
    return result


def read_json(raw):
    return json.loads(
        raw,
        object_pairs_hook=reject_duplicates,
        parse_constant=lambda value: (_ for _ in ()).throw(ValueError(value)),
    )


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


@dataclass(frozen=True)
class ApprovedFile:
    path: PurePosixPath
    raw: bytes
    digest: str

    @property
    def name(self):
        return self.path.name

    @property
    def suffix(self):
        return self.path.suffix


def relative_path(name):
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


def checked_file(root, record):
    path = relative_path(record.get("path"))
    target = root.joinpath(*path.parts)
    # Resolve the root (macOS /tmp may itself be a symlink), reject links inside the bundle.
    if root.is_symlink() or any(
        p.is_symlink() for p in [target, *target.parents] if p == root or p.is_relative_to(root)
    ):
        raise ValueError("candidate_symlink")
    raw = target.read_bytes()
    if (
        type(record.get("bytes")) is not int
        or record["bytes"] != len(raw)
        or record.get("sha256") != sha(raw)
    ):
        raise ValueError("candidate_hash_mismatch")
    return ApprovedFile(path, raw, record["sha256"])


def file_record(path, raw):
    return {"path": str(path), "bytes": len(raw), "sha256": sha(raw)}


def artifact_files(root, data, reader=checked_file):
    files = {}
    for record in data.get("artifacts", []):
        role = record["role"]
        if role not in ROLES or role in files:
            raise ValueError("candidate_artifact_roles")
        files[role] = reader(root, record)
    if (
        set(files) != ROLES
        or len({f.path for f in files.values()}) != len(ROLES)
        or len({f.name for f in files.values()}) != len(ROLES)
    ):
        raise ValueError("candidate_artifact_set")
    return files


def source_identity(data, commit):
    if not re.fullmatch(r"[0-9a-f]{40}", commit) or data.get("source_commit") != commit:
        raise ValueError("candidate_commit_mismatch")
    if data.get("source_dirty") is not False:
        raise ValueError("candidate_source_dirty")
    if data.get("version") != "0.0.1":
        raise ValueError("development_or_unfrozen_version")


def platform_target(system, architecture):
    return {
        ("linux", "x86_64"): "linux-x64",
        ("win32", "amd64"): "windows-x64",
        ("darwin", "arm64"): "macos-arm64",
    }.get((system, architecture.lower()))


def validate_installation(evidence, build, build_sha, files):
    target = platform_target(evidence.get("platform"), evidence.get("architecture", ""))
    expected = [{"name": n, "returncode": 1 if n == "uvx-cold-offline" else 0} for n in COMMANDS]
    if (
        evidence.get("kind") != "installed_safe_checks.v2"
        or evidence.get("status") != "passed"
        or evidence.get("completed") is not True
        or evidence.get("source_commit") != build["source_commit"]
        or evidence.get("build_manifest_sha256") != build_sha
        or evidence.get("artifact_sha256") != {r: f.digest for r, f in files.items()}
        or evidence.get("version") != build["version"]
        or evidence.get("scope") != SCOPE
        or evidence.get("checks") != expected
        or any(type(row.get("returncode")) is not int for row in evidence.get("checks", []))
        or type(evidence.get("commands")) is not int
        or evidence["commands"] != len(COMMANDS)
        or evidence.get("python") != "3.14.7"
        or not target
        or type(evidence.get("model_requests_sent")) is not int
        or evidence.get("model_requests_sent") != 0
        or evidence.get("host_lock_tests") != "not_run"
    ):
        raise ValueError("installation_evidence_mismatch")
    return target
