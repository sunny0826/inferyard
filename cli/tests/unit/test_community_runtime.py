"""Synthetic profiles exercise rejection logic; they never become production trust roots."""

import copy
import hashlib
import json
import subprocess
import zipfile
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.application.types import CommandRequest
from inferyard.config import preparation_io
from inferyard.config.preparation_io import PreparationError, json_bytes
from inferyard.platforms import runtime_verification, windows_runtime_prepare
from inferyard.platforms.runtime_archive import member_records, zip_entries
from inferyard.platforms.runtime_profiles import (
    PROFILES,
    load_profile,
    output_files,
    profile_header,
)


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
    # Inject only in this fixture; the actual profile loader still rejects these synthetic roots.
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


def test_prepared_receipt_uses_relative_paths_and_can_move_as_a_tree(synthetic_runtime, tmp_path):
    request, profile = synthetic_runtime
    result = windows_runtime_prepare.prepare(request)
    receipt_path = Path(result["receipt"])
    receipt = json.loads(receipt_path.read_text())
    assert receipt["engine_binary"] == profile["engine_binary"]
    assert not Path(receipt["engine_binary"]).is_absolute()
    assert receipt["ready_to_run"] is False and receipt["model_requests_sent"] == 0
    moved = tmp_path / "moved"
    request.out.rename(moved)
    _, engine, _ = runtime_verification.verify_receipt(moved / receipt_path.name, mode="cuda")
    assert engine == moved / profile["engine_binary"]


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
    request, profile = synthetic_runtime
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


def test_archive_hash_and_missing_runtime_block_before_writing(synthetic_runtime):
    from dataclasses import replace

    request, _ = synthetic_runtime
    with pytest.raises(PreparationError, match="runtime_archive_required"):
        windows_runtime_prepare.prepare(replace(request, runtime_archive=None))
    request.archive.write_bytes(b"not the trusted archive")
    with pytest.raises(PreparationError, match="archive_identity_mismatch"):
        windows_runtime_prepare.prepare(request)
    assert not request.out.exists()


@pytest.mark.parametrize(
    "names",
    [
        ["../escape"],
        ["C:/escape"],
        ["a\\b"],
        ["CON.dll"],
        ["a."],
        ["x", "X"],
        ["a", "a/b"],
        ["A/b", "a/c"],
    ],
)
def test_unsafe_zip_entries_are_rejected(tmp_path, names):
    path = tmp_path / "unsafe.zip"
    with zipfile.ZipFile(path, "w") as package:
        for name in names:
            package.writestr(name, b"x")
    with (
        zipfile.ZipFile(path) as package,
        pytest.raises(PreparationError, match="unsafe_archive_entry"),
    ):
        zip_entries(package)


def test_cuda_conflicting_library_hashes_are_rejected():
    members = [
        dict(role="engine", path="llama-server.exe", bytes=1, sha256="a" * 64),
        dict(role="engine", path="cuda.dll", bytes=1, sha256="b" * 64),
        dict(role="runtime", path="cuda.dll", bytes=1, sha256="c" * 64),
    ]
    with pytest.raises(PreparationError, match="runtime_library_collision"):
        output_files(members)
    members[-1]["sha256"] = "b" * 64
    assert output_files(members)[2]["cuda.dll"] == "b" * 64


@pytest.mark.parametrize("failure", ["timeout", "nonzero", "version", "cancel"])
def test_version_failure_never_creates_success_receipt(synthetic_runtime, monkeypatch, failure):
    request, _ = synthetic_runtime
    monkeypatch.setattr(
        windows_runtime_prepare, "query_version", runtime_verification.query_version
    )

    def run(*args, **kwargs):
        assert kwargs["timeout"] == 10 and args[0][-1] == "--version"
        if failure == "timeout":
            raise subprocess.TimeoutExpired(args[0], 10)
        if failure == "cancel":
            raise KeyboardInterrupt
        return SimpleNamespace(
            returncode=1 if failure == "nonzero" else 0, stdout="unknown", stderr=""
        )

    monkeypatch.setattr(runtime_verification.subprocess, "run", run)
    with pytest.raises(KeyboardInterrupt if failure == "cancel" else PreparationError):
        windows_runtime_prepare.prepare(request)
    assert not (request.out / "runtime-receipt.json").exists()


def test_committed_profiles_have_complete_non_synthetic_archive_members():
    for name in PROFILES:
        profile, digest = load_profile(name)
        assert len(digest) == 64
        assert len(profile["members"]) > 30
        assert len(profile["libraries"]) > 25
        assert profile["archives"] == profile_header(name)["archives"]


@pytest.mark.parametrize("change", ["float", "bool", "extra", "missing"])
def test_nested_receipt_archive_types_are_exact(synthetic_runtime, change):
    request, _ = synthetic_runtime
    result = windows_runtime_prepare.prepare(request)
    path = Path(result["receipt"])
    receipt = json.loads(path.read_bytes())
    archive = receipt["archives"][0]
    if change == "float":
        archive["bytes"] = float(archive["bytes"])
    elif change == "bool":
        archive["bytes"] = True
    elif change == "extra":
        archive["extra"] = "unapproved"
    else:
        del archive["source_url"]
    path.write_bytes(json_bytes(receipt))
    with pytest.raises(PreparationError, match="runtime_profile_mismatch"):
        runtime_verification.verify_receipt(path, mode="cuda")


@pytest.mark.parametrize("change", ["float", "bool", "extra", "missing"])
def test_nested_packaged_profile_archive_types_are_exact(change):
    from inferyard.platforms.runtime_profiles import validate_profile

    name = next(iter(PROFILES))
    profile, _ = load_profile(name)
    archive = profile["archives"][0]
    if change == "float":
        archive["bytes"] = float(archive["bytes"])
    elif change == "bool":
        archive["bytes"] = True
    elif change == "extra":
        archive["extra"] = "unapproved"
    else:
        del archive["source_url"]
    with pytest.raises(ValueError):
        validate_profile(profile, name)
