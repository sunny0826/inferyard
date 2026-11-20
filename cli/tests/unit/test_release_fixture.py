"""Tiny synthetic licensed distribution; never a real release or license decision."""

import io
import json
import tarfile
import tomllib
import zipfile
from pathlib import Path

from inferyard.contracts.comparison_types import JSONRecord

from scripts.check_distribution import inspect_sdist, inspect_wheel
from scripts.export_runtime_constraints import validate
from scripts.release_common import (
    COMMANDS,
    PROBES,
    SCOPE,
    STUDIO_KIND,
    URLS,
    canonical_sha,
    expected_checks,
    file_record,
    sha,
    studio_report_schema,
    wheel_resource_files,
)

COMMIT = "a" * 40
VERSION = "0.0.5"


def _fixture_texts() -> tuple[str, str]:
    project_text = """[project]
name = "inferyard"
version = "0.0.5"
requires-python = "==3.15.*"
dependencies = []
license = "MIT"
license-files = ["LICENSE"]
[project.scripts]
inferyard = "inferyard.cli:main"
[project.urls]
""" + "\n".join(f'{k} = "{v}"' for k, v in URLS.items())
    lock_text = '[[package]]\nname = "inferyard"\nversion = "0.0.5"\ndependencies = []\n'
    return project_text, lock_text


def _metadata_bytes() -> bytes:
    return (
        f"Name: inferyard\nVersion: {VERSION}\nRequires-Python: ==3.15.*\n"
        "License-Expression: MIT\nLicense-File: LICENSE\n"
        + "".join(f"Project-URL: {k}, {v}\n" for k, v in URLS.items())
    ).encode()


def _wheel_bytes(metadata: bytes, license_raw: bytes, init: bytes) -> bytes:
    wheel = io.BytesIO()
    with zipfile.ZipFile(wheel, "w") as archive:
        archive.writestr("inferyard/__init__.py", init)
        prefix = f"inferyard-{VERSION}.dist-info/"
        archive.writestr(prefix + "METADATA", metadata)
        archive.writestr(prefix + "licenses/LICENSE", license_raw)
        archive.writestr(
            prefix + "entry_points.txt", "[console_scripts]\ninferyard = inferyard.cli:main\n"
        )
    return wheel.getvalue()


def _sdist_bytes(source: dict[str, bytes], metadata: bytes, init: bytes) -> bytes:
    sdist = io.BytesIO()
    with tarfile.open(fileobj=sdist, mode="w:gz") as archive:
        for name, raw in {
            **source,
            "PKG-INFO": metadata,
            "src/inferyard/__init__.py": init,
        }.items():
            item = tarfile.TarInfo(f"inferyard-{VERSION}/{name}")
            item.size = len(raw)
            archive.addfile(item, io.BytesIO(raw))
    return sdist.getvalue()


def _write_artifacts(
    root: Path, wheel: bytes, sdist: bytes, proof: JSONRecord
) -> list[JSONRecord]:
    artifact_bytes = {
        "wheel": (f"inferyard-{VERSION}-py3-none-any.whl", wheel),
        "sdist": (f"inferyard-{VERSION}.tar.gz", sdist),
        "constraints": ("constraints.txt", b""),
    }
    artifacts = []
    for role, (name, raw) in artifact_bytes.items():
        (root / name).write_bytes(raw)
        artifacts.append({"role": role, **file_record(name, raw)})
    constraints = root / "constraints.json"
    with constraints.open("w", encoding="utf-8") as stream:
        json.dump(proof, stream)
    raw = constraints.read_bytes()
    artifacts.append({"role": "constraints_validation", **file_record(constraints.name, raw)})
    return artifacts


def _manifest_record(
    artifacts: list[JSONRecord],
    resources: dict[str, str],
    source: dict[str, bytes],
    checks: dict[str, JSONRecord],
) -> JSONRecord:
    return dict(
        kind="community_distribution.v2",
        candidate_status="build_checked",
        source_dirty=False,
        source_commit=COMMIT,
        version=VERSION,
        artifacts=artifacts,
        resources=resources,
        license="MIT",
        project_urls=URLS,
        source_files={n: sha(r) for n, r in source.items()},
        checks=checks,
    )


def _probe_records(wheel: bytes) -> dict[str, JSONRecord]:
    return {
        name: {
            "stdout_sha256": sha(f"synthetic {name} output".encode()),
            "current_studio_report_kind": STUDIO_KIND,
            "unsupported_report_formats_rejected": list(range(1, 9)),
            "resource_files": wheel_resource_files(wheel),
            "tool_source_hash": sha(b"synthetic tool identity"),
            "scorer_hash": sha(b"synthetic scorer identity"),
        }
        for name in PROBES
    }


def _installed_evidence(
    artifacts: list[JSONRecord], manifest_sha256: str, wheel: bytes
) -> JSONRecord:
    schema = studio_report_schema()
    return dict(
        kind="installed_safe_checks.v4",
        probe_checks=_probe_records(wheel),
        studio_report_schema={
            "$id": schema["$id"],
            "argv": ["/synthetic/bin/inferyard", "--schema", "studio_report"],
            "stdout_sha256": sha(b"synthetic schema stdout"),
            "canonical_json_sha256": canonical_sha(schema),
        },
        studio_json={
            "path": "report/studio.json",
            "sha256": sha(b"synthetic studio json"),
            "schema_validated": True,
            "verify_exit_code": 4,
            "verify_limitations": ["studio_report_not_verifiable"],
        },
        status="passed",
        completed=True,
        source_commit=COMMIT,
        build_manifest_sha256=manifest_sha256,
        artifact_sha256={r["role"]: r["sha256"] for r in artifacts},
        version=VERSION,
        platform="darwin",
        architecture="arm64",
        python="3.15.2",
        commands=len(COMMANDS),
        scope=SCOPE,
        model_requests_sent=0,
        host_lock_tests="not_run",
        wheel_sha256=sha(wheel),
        checks=expected_checks(),
    )


def build_fixture(root: Path) -> tuple[Path, JSONRecord, Path, JSONRecord]:
    root.mkdir(exist_ok=True)
    project_text, lock_text = _fixture_texts()
    project = tomllib.loads(project_text)["project"]
    lock = tomllib.loads(lock_text)
    license_raw = b"Synthetic license bytes for tests only, not a license grant.\n"
    init = b'__version__ = "0.0.5"\n'
    metadata = _metadata_bytes()
    source = {
        "pyproject.toml": project_text.encode(),
        "uv.lock": lock_text.encode(),
        "LICENSE": license_raw,
        "README.md": b"fixture",
        ".gitignore": b"dist/\n",
    }
    wheel = _wheel_bytes(metadata, license_raw, init)
    sdist = _sdist_bytes(source, metadata, init)
    proof = validate("", lock, project)
    artifacts = _write_artifacts(root, wheel, sdist, proof)
    resources = {"__init__.py": sha(init)}
    wheel_check = inspect_wheel(io.BytesIO(wheel), resources, project, license_raw)
    checks = {
        "wheel": wheel_check,
        "sdist": inspect_sdist(
            io.BytesIO(sdist), resources, project=project, license_raw=license_raw
        ),
        "rebuilt_wheel": wheel_check,
        "constraints": proof,
    }
    data = _manifest_record(artifacts, resources, source, checks)
    manifest = root / "manifest.json"
    with manifest.open("w", encoding="utf-8") as stream:
        json.dump(data, stream)
    evidence = _installed_evidence(artifacts, sha(manifest.read_bytes()), wheel)
    installed = root / "installed.json"
    with installed.open("w", encoding="utf-8") as stream:
        json.dump(evidence, stream)
    return manifest, data, installed, evidence
