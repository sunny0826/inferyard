"""Tiny synthetic licensed distribution; never a real release or license decision."""

import io
import json
import tarfile
import zipfile

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
VERSION = "0.0.3"


def build_fixture(root):
    import tomllib

    root.mkdir(exist_ok=True)
    project_text = """[project]
name = "inferyard"
version = "0.0.3"
requires-python = "==3.15.*"
dependencies = []
license = "MIT"
license-files = ["LICENSE"]
[project.scripts]
inferyard = "inferyard.cli:main"
[project.urls]
""" + "\n".join(f'{k} = "{v}"' for k, v in URLS.items())
    lock_text = '[[package]]\nname = "inferyard"\nversion = "0.0.3"\ndependencies = []\n'
    project = tomllib.loads(project_text)["project"]
    lock = tomllib.loads(lock_text)
    license_raw = b"Synthetic license bytes for tests only, not a license grant.\n"
    metadata = (
        f"Name: inferyard\nVersion: {VERSION}\nRequires-Python: ==3.15.*\n"
        "License-Expression: MIT\nLicense-File: LICENSE\n"
        + "".join(f"Project-URL: {k}, {v}\n" for k, v in URLS.items())
    ).encode()
    init = b'__version__ = "0.0.3"\n'
    resources = {"__init__.py": sha(init)}
    wheel = io.BytesIO()
    with zipfile.ZipFile(wheel, "w") as archive:
        archive.writestr("inferyard/__init__.py", init)
        prefix = f"inferyard-{VERSION}.dist-info/"
        archive.writestr(prefix + "METADATA", metadata)
        archive.writestr(prefix + "licenses/LICENSE", license_raw)
        archive.writestr(
            prefix + "entry_points.txt", "[console_scripts]\ninferyard = inferyard.cli:main\n"
        )
    source = {
        "pyproject.toml": project_text.encode(),
        "uv.lock": lock_text.encode(),
        "LICENSE": license_raw,
        "README.md": b"fixture",
        "docs/installation.md": b"fixture",
        ".gitignore": b"dist/\n",
    }
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
    proof = validate("", lock, project)
    artifact_bytes = {
        "wheel": (f"inferyard-{VERSION}-py3-none-any.whl", wheel.getvalue()),
        "sdist": (f"inferyard-{VERSION}.tar.gz", sdist.getvalue()),
        "constraints": ("constraints.txt", b""),
        "constraints_validation": ("constraints.json", json.dumps(proof).encode()),
    }
    artifacts = []
    for role, (name, raw) in artifact_bytes.items():
        (root / name).write_bytes(raw)
        artifacts.append({"role": role, **file_record(name, raw)})
    wheel_check = inspect_wheel(io.BytesIO(wheel.getvalue()), resources, project, license_raw)
    sdist_check = inspect_sdist(
        io.BytesIO(sdist.getvalue()), resources, project=project, license_raw=license_raw
    )
    data = dict(
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
        checks=dict(
            wheel=wheel_check, sdist=sdist_check, rebuilt_wheel=wheel_check, constraints=proof
        ),
    )
    manifest = root / "manifest.json"
    manifest.write_text(json.dumps(data), encoding="utf-8")
    evidence = dict(
        kind="installed_safe_checks.v4",
        probe_checks={
            name: {
                "stdout_sha256": sha(f"synthetic {name} output".encode()),
                "current_studio_report_kind": STUDIO_KIND,
                "unsupported_report_formats_rejected": list(range(1, 9)),
                "resource_files": wheel_resource_files(wheel.getvalue()),
                "tool_source_hash": sha(b"synthetic tool identity"),
                "scorer_hash": sha(b"synthetic scorer identity"),
            }
            for name in PROBES
        },
        studio_report_schema={
            "$id": studio_report_schema()["$id"],
            "argv": ["/synthetic/bin/inferyard", "--schema", "studio_report"],
            "stdout_sha256": sha(b"synthetic schema stdout"),
            "canonical_json_sha256": canonical_sha(studio_report_schema()),
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
        build_manifest_sha256=sha(manifest.read_bytes()),
        artifact_sha256={r["role"]: r["sha256"] for r in artifacts},
        version=VERSION,
        platform="darwin",
        architecture="arm64",
        python="3.15.2",
        commands=len(COMMANDS),
        scope=SCOPE,
        model_requests_sent=0,
        host_lock_tests="not_run",
        wheel_sha256=sha(wheel.getvalue()),
        checks=expected_checks(),
    )
    installed = root / "installed.json"
    installed.write_text(json.dumps(evidence), encoding="utf-8")
    return manifest, data, installed, evidence
