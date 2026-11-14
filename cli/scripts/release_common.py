"""Byte snapshots and installation scope shared by release producers and consumers."""

import hashlib
import io
import json
import re
import zipfile
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
    "schema-studio-report",
    "catalogue",
    "engines",
    "init",
    "probe-install-a",
    "report",
    "verify",
    "verify-legacy-report",
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
# Non-zero exits are part of the contract: a cold offline uvx must fail, the current
# Studio document is deliberately unverifiable, and a retired report layout is rejected.
EXIT_CODES = {"uvx-cold-offline": 1, "verify": 4, "verify-legacy-report": 2}
SCOPE = [
    "offline_core",
    "workspace_init",
    "resources_identity",
    "studio_json_schema",
    "current_format_json",
    "unsupported_format_rejection",
    "uv_uvx_isolation",
    "synthetic_upgrade_rollback",
]
PROBES = ("probe-install-a", "probe-install-b", "probe-rollback")
STUDIO_SCHEMA = "studio_report"
STUDIO_KIND = "studio-report.v1"
STUDIO_VERIFY_LIMITATION = "studio_report_not_verifiable"
RETIRED_REPORT_FORMATS = tuple(range(1, 9))
HEX_DIGEST = re.compile(r"[0-9a-f]{64}")


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


def canonical_json(value):
    return json.dumps(
        value, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False
    ).encode("utf-8")


def canonical_sha(value):
    return sha(canonical_json(value))


def studio_report_schema():
    """Return the authoritative schema the installed CLI must export."""
    from inferyard.contracts.schemas import export_schema

    return export_schema(STUDIO_SCHEMA)


def expected_checks():
    return [{"name": name, "returncode": EXIT_CODES.get(name, 0)} for name in COMMANDS]


def wheel_resource_files(wheel_raw):
    with zipfile.ZipFile(io.BytesIO(wheel_raw)) as archive:
        return sum(
            1
            for name in archive.namelist()
            if name.startswith("inferyard/") and not name.endswith("/")
        )


def expected_probe_check():
    return {
        "current_studio_report_kind": STUDIO_KIND,
        "unsupported_report_formats_rejected": list(RETIRED_REPORT_FORMATS),
    }


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
    if data.get("version") != "0.0.3":
        raise ValueError("development_or_unfrozen_version")


def platform_target(system, architecture):
    return {
        ("linux", "x86_64"): "linux-x64",
        ("win32", "amd64"): "windows-x64",
        ("darwin", "arm64"): "macos-arm64",
    }.get((system, architecture.lower()))


def _digest(value):
    return isinstance(value, str) and HEX_DIGEST.fullmatch(value) is not None


def _validate_probe_checks(evidence, files):
    checks = evidence.get("probe_checks")
    if type(checks) is not dict or set(checks) != set(PROBES):
        raise ValueError("installation_evidence_probe_checks")
    expected = expected_probe_check()
    resources = wheel_resource_files(files["wheel"].raw)
    for name in PROBES:
        row = checks[name]
        if type(row) is not dict or set(row) != {
            "stdout_sha256",
            "current_studio_report_kind",
            "unsupported_report_formats_rejected",
            "resource_files",
            "tool_source_hash",
            "scorer_hash",
        }:
            raise ValueError("installation_evidence_probe_checks")
        if (
            not _digest(row["stdout_sha256"])
            or not _digest(row["tool_source_hash"])
            or not _digest(row["scorer_hash"])
            or row["current_studio_report_kind"] != expected["current_studio_report_kind"]
            or row["unsupported_report_formats_rejected"]
            != expected["unsupported_report_formats_rejected"]
            or any(
                type(version) is not int
                for version in row["unsupported_report_formats_rejected"]
            )
            or type(row["resource_files"]) is not int
            or row["resource_files"] != resources
        ):
            raise ValueError("installation_evidence_probe_checks")


def _validate_studio_schema(evidence):
    schema = studio_report_schema()
    record = evidence.get("studio_report_schema")
    if type(record) is not dict or set(record) != {
        "$id",
        "argv",
        "stdout_sha256",
        "canonical_json_sha256",
    }:
        raise ValueError("installation_evidence_studio_schema")
    argv = record["argv"]
    if (
        record["$id"] != schema["$id"]
        or record["canonical_json_sha256"] != canonical_sha(schema)
        or not _digest(record["stdout_sha256"])
        or type(argv) is not list
        or not argv
        or any(type(item) is not str for item in argv)
        or argv[-2:] != ["--schema", STUDIO_SCHEMA]
    ):
        raise ValueError("installation_evidence_studio_schema")


def _validate_studio_json(evidence):
    record = evidence.get("studio_json")
    if type(record) is not dict or set(record) != {
        "path",
        "sha256",
        "schema_validated",
        "verify_exit_code",
        "verify_limitations",
    }:
        raise ValueError("installation_evidence_studio_json")
    if (
        record["path"] != "report/studio.json"
        or not _digest(record["sha256"])
        or record["schema_validated"] is not True
        or record["verify_exit_code"] != EXIT_CODES["verify"]
        or record["verify_limitations"] != [STUDIO_VERIFY_LIMITATION]
    ):
        raise ValueError("installation_evidence_studio_json")


def validate_installation(evidence, build, build_sha, files):
    target = platform_target(evidence.get("platform"), evidence.get("architecture", ""))
    if (
        evidence.get("kind") != "installed_safe_checks.v4"
        or evidence.get("status") != "passed"
        or evidence.get("completed") is not True
        or evidence.get("source_commit") != build["source_commit"]
        or evidence.get("build_manifest_sha256") != build_sha
        or evidence.get("artifact_sha256") != {r: f.digest for r, f in files.items()}
        or evidence.get("wheel_sha256") != files["wheel"].digest
        or evidence.get("version") != build["version"]
        or evidence.get("scope") != SCOPE
        or evidence.get("checks") != expected_checks()
        or any(type(row.get("returncode")) is not int for row in evidence.get("checks", []))
        or type(evidence.get("commands")) is not int
        or evidence["commands"] != len(COMMANDS)
        or evidence.get("python") != "3.15.2"
        or not target
        or type(evidence.get("model_requests_sent")) is not int
        or evidence.get("model_requests_sent") != 0
        or evidence.get("host_lock_tests") != "not_run"
    ):
        raise ValueError("installation_evidence_mismatch")
    _validate_studio_schema(evidence)
    _validate_studio_json(evidence)
    _validate_probe_checks(evidence, files)
    return target
