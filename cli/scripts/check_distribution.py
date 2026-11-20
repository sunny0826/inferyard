"""Inspect explicit wheel/sdist files and record non-release artifact hashes; no upload."""

import argparse
import configparser
import hashlib
import json
import subprocess
import tarfile
import tomllib
import zipfile
from email.parser import BytesParser
from io import BytesIO
from pathlib import Path, PurePosixPath

from packaging.requirements import Requirement

if __package__:
    from .export_runtime_constraints import validate
    from .release_common import read_json, relative_path
else:
    from export_runtime_constraints import validate
    from release_common import read_json, relative_path

ROOT = Path(__file__).resolve().parents[1]
SDIST_FILES = {
    "pyproject.toml",
    "uv.lock",
    "README.md",
    "PKG-INFO",
    ".gitignore",
}
DATA_FILES = {
    "data/" + name
    for name in (
        "metrics.json",
        "methods.json",
    )
}
COMMUNITY_FILES = {
    "data/community/" + name
    for name in (
        "README.md",
        "configs/linux.example.toml",
        "configs/macos.example.toml",
        "configs/windows.example.toml",
        "bundles/zh-core.json",
        "bundles/zh-context-speed.json",
        "bundles/zh-smoke.json",
        "bundles/zh-svg-pelican.json",
        "profile/prism-b10743-adfffbe-win-cpu-x64.json",
        "profile/prism-b10743-adfffbe-win-cuda-12.4-x64.json",
    )
}


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def source_resources(root):
    source = root / "src/inferyard"
    for path in source.rglob("*"):
        if not path.is_file() or "__pycache__" in path.parts:
            continue
        name = path.relative_to(source).as_posix()
        if path.is_symlink() or not (
            path.suffix == ".py" or name in DATA_FILES | COMMUNITY_FILES
        ):
            raise ValueError("unexpected_source_resource:" + name)
    return {
        p.relative_to(source).as_posix(): digest(p.read_bytes())
        for p in source.rglob("*")
        if p.is_file() and "__pycache__" not in p.parts and p.suffix not in (".pyc", ".pyo")
    }


def check_metadata(metadata, project):
    if metadata["Name"] != project["name"] or metadata["Version"] != project["version"]:
        raise ValueError("package_metadata_mismatch")
    declared = {Requirement(value) for value in project["dependencies"]}
    actual = {Requirement(value) for value in metadata.get_all("Requires-Dist", [])}
    if actual != declared or metadata["Requires-Python"] != project["requires-python"]:
        raise ValueError("package_dependency_metadata_mismatch")
    if metadata["License-Expression"] != project.get("license") or metadata.get_all(
        "License-File", []
    ) != project.get("license-files", []):
        raise ValueError("package_license_metadata_mismatch")
    urls = {f"{key}, {value}" for key, value in project.get("urls", {}).items()}
    if set(metadata.get_all("Project-URL", [])) != urls:
        raise ValueError("package_url_metadata_mismatch")


def validate_resources(expected):
    for name, digest_value in expected.items():
        path = relative_path(name)
        if not (
            path.suffix == ".py" or name in DATA_FILES | COMMUNITY_FILES
        ):
            raise ValueError("unexpected_package_resource:" + name)
        if not isinstance(digest_value, str) or len(digest_value) != 64:
            raise ValueError("invalid_resource_digest")


def inspect_wheel(path, expected, project, license_raw=None):
    validate_resources(expected)
    with zipfile.ZipFile(path) as package:
        names = package.namelist()
        if len(names) != len(set(names)):
            raise ValueError("duplicate_wheel_member")
        resources = {}
        metadata = []
        info_root = f"inferyard-{project['version']}.dist-info/"
        allowed_info = {
            info_root + name for name in ("METADATA", "WHEEL", "RECORD", "entry_points.txt")
        }
        if license_raw is not None:
            license_name = info_root + "licenses/LICENSE"
            allowed_info.add(license_name)
            if package.read(license_name) != license_raw:
                raise ValueError("wheel_license_drift")
        for name in names:
            relative_path(name.rstrip("/"))
            if name.endswith("/"):
                continue
            if name.startswith("inferyard/"):
                relative = name.removeprefix("inferyard/")
                if relative not in expected:
                    raise ValueError("unexpected_wheel_resource:" + name)
                resources[relative] = digest(package.read(name))
            elif name not in allowed_info:
                raise ValueError("unexpected_wheel_member:" + name)
            elif name.endswith("/METADATA"):
                metadata.append(BytesParser().parsebytes(package.read(name)))
        if resources != expected or len(metadata) != 1:
            raise ValueError("wheel_resource_drift")
        check_metadata(metadata[0], project)
        info = f"inferyard-{project['version']}.dist-info/entry_points.txt"
        entries = configparser.ConfigParser()
        entries.read_string(package.read(info).decode("utf-8"))
        if entries.sections() != ["console_scripts"] or dict(entries["console_scripts"]) != {
            "inferyard": "inferyard.cli:main"
        }:
            raise ValueError("entry_point_mismatch")
    return {"resources": len(resources), "requires_dist": metadata[0].get_all("Requires-Dist", [])}


def inspect_sdist(path, expected, *, project=None, license_raw=None):
    resources, seen = {}, set()
    allowed = SDIST_FILES | ({"LICENSE"} if license_raw is not None else set())
    kwargs = {"fileobj": path} if hasattr(path, "read") else {"name": path}
    with tarfile.open(mode="r:gz", **kwargs) as package:
        for member in package.getmembers():
            relative_path(member.name)
            parts = PurePosixPath(member.name).parts
            if project is not None and parts[0] != f"inferyard-{project['version']}":
                raise ValueError("sdist_root_version_mismatch")
            if len(parts) < 2 or ".." in parts or PurePosixPath(member.name).is_absolute():
                raise ValueError("unsafe_sdist_member")
            relative = "/".join(parts[1:])
            if relative in seen or not member.isfile():
                raise ValueError("duplicate_or_special_sdist_member")
            seen.add(relative)
            if relative.startswith("src/inferyard/"):
                resource = relative.removeprefix("src/inferyard/")
                if resource not in expected:
                    raise ValueError("unexpected_sdist_resource")
                resources[resource] = digest(package.extractfile(member).read())
            elif relative not in allowed:
                raise ValueError("unexpected_sdist_member:" + relative)
            if relative == "LICENSE" and package.extractfile(member).read() != license_raw:
                raise ValueError("sdist_license_drift")
            if relative == "PKG-INFO" and project is not None:
                check_metadata(
                    BytesParser().parsebytes(package.extractfile(member).read()), project
                )
    if resources != expected or not allowed <= seen:
        raise ValueError("sdist_resource_drift")
    return {"files": len(seen), "resources": len(resources)}


def _parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--wheel", type=Path, required=True)
    parser.add_argument("--sdist", type=Path, required=True)
    parser.add_argument("--constraints", type=Path, required=True)
    parser.add_argument(
        "--out", type=Path, required=True, help="new manifest, never a release approval"
    )
    parser.add_argument("--rebuilt-wheel", type=Path)
    return parser.parse_args()


def _sdist_source_files(
    sdist_raw: bytes, version: str, license_raw: bytes | None
) -> dict[str, str]:
    source_files: dict[str, str] = {}
    names = (
        "pyproject.toml",
        "uv.lock",
        "README.md",
        ".gitignore",
        *(("LICENSE",) if license_raw is not None else ()),
    )
    with tarfile.open(fileobj=BytesIO(sdist_raw), mode="r:gz") as archive:
        for name in names:
            raw = (ROOT / name).read_bytes()
            extracted = archive.extractfile(f"inferyard-{version}/{name}")
            if extracted is None or extracted.read() != raw:
                raise ValueError("sdist_source_drift:" + name)
            source_files[name] = digest(raw)
    return source_files


def _artifact_records(
    paths: dict[str, Path], snapshots: dict[str, bytes], out_dir: Path
) -> list[dict[str, str | int]]:
    artifacts: list[dict[str, str | int]] = []
    for role, path in paths.items():
        relative = path.resolve().relative_to(out_dir.resolve()).as_posix()
        raw = snapshots[role]
        artifacts.append({"role": role, "path": relative, "bytes": len(raw), "sha256": digest(raw)})
    return artifacts


def _git_source_state() -> tuple[str, bool]:
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    dirty = bool(subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT))
    return commit, dirty


def main() -> None:
    args = _parse_args()
    project = tomllib.loads((ROOT / "pyproject.toml").read_text(encoding="utf-8"))["project"]
    expected = source_resources(ROOT)
    license_raw = (ROOT / "LICENSE").read_bytes() if project.get("license") else None
    validation = args.constraints.with_suffix(args.constraints.suffix + ".validation.json")
    paths = {
        "wheel": args.wheel, "sdist": args.sdist,
        "constraints": args.constraints, "constraints_validation": validation,
    }
    snapshots = {role: path.read_bytes() for role, path in paths.items()}
    checks = {
        "wheel": inspect_wheel(BytesIO(snapshots["wheel"]), expected, project, license_raw),
        "sdist": inspect_sdist(
            BytesIO(snapshots["sdist"]), expected, project=project, license_raw=license_raw
        ),
    }
    if args.rebuilt_wheel:
        rebuilt = BytesIO(args.rebuilt_wheel.read_bytes())
        checks["rebuilt_wheel"] = inspect_wheel(rebuilt, expected, project, license_raw)
    lock = tomllib.loads((ROOT / "uv.lock").read_text(encoding="utf-8"))
    proof = validate(snapshots["constraints"].decode("utf-8"), lock, project)
    if read_json(snapshots["constraints_validation"]) != proof:
        raise ValueError("constraints_validation_drift")
    checks["constraints"] = proof
    commit, dirty = _git_source_state()
    artifacts = _artifact_records(paths, snapshots, args.out.parent)
    platforms = ("linux-x64", "windows-x64", "macos-arm64")
    manifest = {
        "kind": "community_distribution.v2",
        "version": project["version"],
        "candidate_status": "build_checked",
        "source_commit": commit,
        "source_dirty": dirty,
        "artifacts": artifacts,
        "checks": checks,
        "resources": expected,
        "source_files": _sdist_source_files(snapshots["sdist"], project["version"], license_raw),
        "license": project.get("license"),
        "project_urls": project.get("urls", {}),
        "platform_coverage": dict.fromkeys(platforms, "not_verified"),
    }
    with args.out.open("x", encoding="utf-8") as stream:
        json.dump(manifest, stream, indent=2, ensure_ascii=False)
        stream.write("\n")
    print(
        json.dumps(
            {"manifest": str(args.out), "artifacts": artifacts, "candidate_status": "build_checked"}
        )
    )


if __name__ == "__main__":
    main()
