"""Validate v2 build snapshots without rebuilding or reading mutable artifact sources."""

import tarfile
import tomllib
from io import BytesIO

if __package__:
    from .check_distribution import inspect_sdist, inspect_wheel
    from .export_runtime_constraints import validate
    from .release_common import URLS, ApprovedFile, JSONRecord, read_json, sha, source_identity
else:
    from check_distribution import inspect_sdist, inspect_wheel
    from export_runtime_constraints import validate
    from release_common import URLS, ApprovedFile, JSONRecord, read_json, sha, source_identity

SDIST_SOURCE_NAMES = ("pyproject.toml", "uv.lock", "LICENSE", "README.md", ".gitignore")


def _read_sdist_member(archive: tarfile.TarFile, version: str, name: str) -> bytes:
    extracted = archive.extractfile(f"inferyard-{version}/{name}")
    if extracted is None:
        raise ValueError("missing_sdist_member:" + name)
    return extracted.read()


def _locked_inferyard_version(lock: JSONRecord) -> str | None:
    packages = lock.get("package")
    if not isinstance(packages, list):
        return None
    versions = [
        p.get("version")
        for p in packages
        if isinstance(p, dict)
        and p.get("name") == "inferyard"
        and isinstance(p.get("version"), str)
    ]
    return versions[0] if len(versions) == 1 else None


def _validate_project_metadata(project: JSONRecord, lock: JSONRecord, version: str) -> None:
    if project.get("license") not in {"MIT", "Apache-2.0"} or project.get("license-files") != [
        "LICENSE"
    ]:
        raise ValueError("publication_license_pending")
    if (
        project.get("name") != "inferyard"
        or project.get("version") != version
        or _locked_inferyard_version(lock) != version
        or project.get("scripts") != {"inferyard": "inferyard.cli:main"}
    ):
        raise ValueError("candidate_metadata_version_mismatch")


def _validate_sdist_sources(
    data: JSONRecord, sdist_raw: bytes, version: str
) -> tuple[JSONRecord, JSONRecord, bytes]:
    with tarfile.open(fileobj=BytesIO(sdist_raw), mode="r:gz") as archive:
        project = tomllib.loads(_read_sdist_member(archive, version, "pyproject.toml").decode())[
            "project"
        ]
        lock = tomllib.loads(_read_sdist_member(archive, version, "uv.lock").decode())
        _validate_project_metadata(project, lock, version)
        license_raw = _read_sdist_member(archive, version, "LICENSE")
        if not license_raw.strip() or data.get("license") != project.get("license"):
            raise ValueError("candidate_license_drift")
        if project.get("urls") != URLS or data.get("project_urls") != URLS:
            raise ValueError("candidate_url_drift")
        init = _read_sdist_member(archive, version, "src/inferyard/__init__.py").decode()
        if f'__version__ = "{version}"' not in init:
            raise ValueError("candidate_init_version_mismatch")
        expected_sources = {
            name: sha(_read_sdist_member(archive, version, name)) for name in SDIST_SOURCE_NAMES
        }
        if data.get("source_files") != expected_sources:
            raise ValueError("candidate_source_files_mismatch")
    return project, lock, license_raw


def validate_build(data: JSONRecord, files: dict[str, ApprovedFile], commit: str) -> None:
    if (
        data.get("kind") != "community_distribution.v2"
        or data.get("candidate_status") != "build_checked"
    ):
        raise ValueError("candidate_build_kind")
    source_identity(data, commit)
    version = str(data["version"])
    if (
        files["wheel"].name != f"inferyard-{version}-py3-none-any.whl"
        or files["sdist"].name != f"inferyard-{version}.tar.gz"
    ):
        raise ValueError("candidate_filename_version_mismatch")
    project, lock, license_raw = _validate_sdist_sources(data, files["sdist"].raw, version)
    expected = data.get("resources")
    if not isinstance(expected, dict) or not expected:
        raise ValueError("candidate_resources_missing")
    wheel_check = inspect_wheel(BytesIO(files["wheel"].raw), expected, project, license_raw)
    sdist_check = inspect_sdist(
        BytesIO(files["sdist"].raw), expected, project=project, license_raw=license_raw
    )
    proof = validate(files["constraints"].raw.decode("utf-8"), lock, project)
    if read_json(files["constraints_validation"].raw) != proof:
        raise ValueError("constraints_validation_drift")
    if data.get("checks") != {
        "wheel": wheel_check,
        "sdist": sdist_check,
        "rebuilt_wheel": wheel_check,
        "constraints": proof,
    }:
        raise ValueError("candidate_checks_missing_or_mismatched")
