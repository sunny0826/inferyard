"""Validate v2 build snapshots without rebuilding or reading mutable artifact sources."""

import tarfile
import tomllib
from io import BytesIO

if __package__:
    from .check_distribution import inspect_sdist, inspect_wheel
    from .export_runtime_constraints import validate
    from .release_common import URLS, read_json, sha, source_identity
else:
    from check_distribution import inspect_sdist, inspect_wheel
    from export_runtime_constraints import validate
    from release_common import URLS, read_json, sha, source_identity


def validate_build(data, files, commit):
    if (
        data.get("kind") != "community_distribution.v2"
        or data.get("candidate_status") != "build_checked"
    ):
        raise ValueError("candidate_build_kind")
    source_identity(data, commit)
    version = data["version"]
    if (
        files["wheel"].name != f"inferyard-{version}-py3-none-any.whl"
        or files["sdist"].name != f"inferyard-{version}.tar.gz"
    ):
        raise ValueError("candidate_filename_version_mismatch")
    with tarfile.open(fileobj=BytesIO(files["sdist"].raw), mode="r:gz") as archive:

        def member(name):
            return archive.extractfile(f"inferyard-{version}/{name}").read()

        project = tomllib.loads(member("pyproject.toml").decode("utf-8"))["project"]
        lock = tomllib.loads(member("uv.lock").decode("utf-8"))
        if project.get("license") not in {"MIT", "Apache-2.0"} or project.get("license-files") != [
            "LICENSE"
        ]:
            raise ValueError("publication_license_pending")
        license_raw = member("LICENSE")
        if not license_raw.strip() or data.get("license") != project["license"]:
            raise ValueError("candidate_license_drift")
        if project.get("urls") != URLS or data.get("project_urls") != URLS:
            raise ValueError("candidate_url_drift")
        locked = [p for p in lock["package"] if p["name"] == "inferyard"]
        if (
            project["name"] != "inferyard"
            or project["version"] != version
            or len(locked) != 1
            or locked[0]["version"] != version
            or project.get("scripts") != {"inferyard": "inferyard.cli:main"}
        ):
            raise ValueError("candidate_metadata_version_mismatch")
        init = member("src/inferyard/__init__.py").decode("utf-8")
        if f'__version__ = "{version}"' not in init:
            raise ValueError("candidate_init_version_mismatch")
        names = {
            "pyproject.toml",
            "uv.lock",
            "LICENSE",
            "README.md",
            "docs/installation.md",
            ".gitignore",
        }
        if data.get("source_files") != {name: sha(member(name)) for name in names}:
            raise ValueError("candidate_source_files_mismatch")
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
