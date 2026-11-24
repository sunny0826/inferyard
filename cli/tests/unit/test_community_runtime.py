"""Synthetic profiles exercise rejection logic; they never become production trust roots."""

import copy
import hashlib
import json
import zipfile
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.provisioning import preparation_io, runtime_verification, windows_runtime_prepare
from inferyard.provisioning.preparation_io import PreparationError, json_bytes
from inferyard.provisioning.runtime_archive import member_records
from inferyard.provisioning.runtime_profiles import PROFILES, output_files, profile_header


@pytest.fixture
def synthetic_runtime(tmp_path, monkeypatch):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Windows", "x64"))
    name = next(n for n, mode in PROFILES.items() if mode == "cuda")
    profile = profile_header(name)
    archives, members = {}, []
    for role, content in [
        ("engine", {"llama-server.exe": b"fake server", "ggml.dll": b"fake ggml"}),
        ("runtime", {"cuda/sub/cudart.dll": b"fake cuda"}),
    ]:
        path = tmp_path / f"{role}.zip"
        with zipfile.ZipFile(path, "w") as package:
            for filename, raw in content.items():
                package.writestr(filename, raw)
        archives[role] = path
        with zipfile.ZipFile(path) as package:
            members.extend(member_records(package, role))
        for archive in profile["archives"]:
            if archive["role"] == role:
                archive["bytes"] = path.stat().st_size
                archive["sha256"] = hashlib.sha256(path.read_bytes()).hexdigest()
    profile["members"] = members
    server, manifest, libraries, _ = output_files(members)
    profile.update(engine_binary=server, runtime_library_manifest=manifest, libraries=libraries)
    digest = hashlib.sha256(json_bytes(profile)).hexdigest()
    for module in (windows_runtime_prepare, runtime_verification):
        monkeypatch.setattr(module, "load_profile", lambda _: (copy.deepcopy(profile), digest))
    monkeypatch.setattr(
        windows_runtime_prepare,
        "query_version",
        lambda *a, **k: {
            "argv": ["--version"],
            "returncode": 0,
            "stdout": "10743 adfffbe41",
            "stderr": "",
        },
    )
    request = CommandRequest(
        "runtime prepare",
        runtime_profile=name,
        archive=archives["engine"],
        runtime_archive=archives["runtime"],
        out=tmp_path / "prepared",
    )
    return request, profile


@pytest.mark.parametrize(
    "change",
    [
        "receipt_hash",
        "manifest_delete",
        "both",
        "binary",
        "extra_dll",
        "missing_dll",
        "escape",
        "mode",
        "bool",
        "version_bool",
        "unknown",
        "duplicate",
    ],
)
def test_receipt_cannot_authorize_changed_assets(synthetic_runtime, change):
    request, _ = synthetic_runtime
    result = windows_runtime_prepare.prepare(request)
    path = Path(result["receipt"])
    receipt = json.loads(path.read_text())
    manifest = Path(result["runtime_library_manifest"])
    if change == "receipt_hash":
        receipt["profile_sha256"] = "0" * 64
    elif change == "manifest_delete":
        manifest.write_text("{}")
    elif change == "both":
        receipt["libraries"] = {}
        manifest.write_text("{}")
    elif change == "binary":
        Path(result["engine"]).write_bytes(b"arbitrary")
    elif change == "extra_dll":
        (Path(result["engine"]).parent / "extra.dll").write_bytes(b"extra")
    elif change == "missing_dll":
        (Path(result["engine"]).parent / "ggml.dll").unlink()
    elif change == "escape":
        receipt["engine_binary"] = "../llama-server.exe"
    elif change == "mode":
        receipt["mode"] = "cpu"
    elif change == "bool":
        receipt["model_requests_sent"] = False
    elif change == "version_bool":
        receipt["version"]["returncode"] = False
    elif change == "unknown":
        receipt["extra"] = True
    path.write_bytes(json_bytes(receipt))
    if change == "duplicate":
        path.write_text(
            path.read_text().replace(
                '"schema_version": 3', '"schema_version": 3, "schema_version": 3'
            )
        )
    with pytest.raises((PreparationError, OSError)):
        runtime_verification.verify_receipt(path, mode="cuda")
