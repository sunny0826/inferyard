"""Download integrity, safe resumes and extraction against small local fixtures."""

import hashlib
import importlib.util
import zipfile
from pathlib import Path

import httpx
import pytest

SPEC = importlib.util.spec_from_file_location(
    "prepare_windows_runtime", Path(__file__).parents[2] / "scripts/prepare_windows_runtime.py"
)
runtime = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runtime)


@pytest.fixture
def setup(monkeypatch, tmp_path):
    # Exercise byte/range semantics on either OS without changing the real D: guard.
    monkeypatch.setattr(runtime, "require_destination", lambda path: path)
    return tmp_path / "model.bin"


def client(monkeypatch, handler):
    original = httpx.Client
    monkeypatch.setattr(
        runtime.httpx,
        "Client",
        lambda **kwargs: original(transport=httpx.MockTransport(handler), **kwargs),
    )


@pytest.mark.parametrize("range_supported", [True, False])
def test_resume_validates_range_or_restarts_when_ignored(setup, monkeypatch, range_supported):
    content = b"frozen-model-content"
    partial = setup.with_suffix(".bin.part")
    partial.write_bytes(content[:6])

    def response(request):
        assert request.headers["range"] == "bytes=6-"
        if range_supported:
            return httpx.Response(
                206,
                content=content[6:],
                headers={"Content-Range": f"bytes 6-{len(content) - 1}/{len(content)}"},
            )
        return httpx.Response(200, content=content)

    client(monkeypatch, response)
    runtime.verified_download(
        "https://source.example/model", setup, len(content), hashlib.sha256(content).hexdigest()
    )
    assert setup.read_bytes() == content and not partial.exists()


def test_wrong_hash_is_not_published_and_existing_file_is_not_overwritten(setup, monkeypatch):
    client(monkeypatch, lambda request: httpx.Response(200, content=b"wrong"))
    with pytest.raises(ValueError, match="download_hash_mismatch"):
        runtime.verified_download("https://source.example/model", setup, 5, "0" * 64)
    assert not setup.exists() and setup.with_suffix(".bin.part").read_bytes() == b"wrong"
    setup.write_bytes(b"existing")
    with pytest.raises(ValueError, match="existing_asset_identity_mismatch"):
        runtime.verified_download("https://source.example/model", setup, 5, "0" * 64)
    assert setup.read_bytes() == b"existing"


def test_mismatched_range_never_appends(setup, monkeypatch):
    partial = setup.with_suffix(".bin.part")
    partial.write_bytes(b"abc")
    client(
        monkeypatch,
        lambda request: httpx.Response(
            206, content=b"def", headers={"Content-Range": "bytes 2-4/6"}
        ),
    )
    with pytest.raises(ValueError, match="download_range_mismatch"):
        runtime.verified_download("https://source.example/model", setup, 6, "0" * 64)
    assert partial.read_bytes() == b"abc" and not setup.exists()


@pytest.mark.parametrize(
    "name", ["../escape.dll", "D:/escape.dll", "lib.dll:stream", "folder\\lib.dll"]
)
def test_unsafe_archive_rejected_before_output(setup, name):
    archive = setup.parent / "engine.zip"
    with zipfile.ZipFile(archive, "w") as package:
        package.writestr(name, b"unsafe")
    if "\\" in name:
        # Windows ZipInfo normalizes names while writing; preserve an actual
        # backslash entry in both ZIP headers to exercise hostile input.
        raw = archive.read_bytes()
        archive.write_bytes(raw.replace(name.replace("\\", "/").encode(), name.encode()))
    destination = setup.parent / "engine"
    with pytest.raises(ValueError, match="unsafe_engine_archive_entry"):
        runtime.unpack(archive, destination)
    assert not destination.exists()


def test_runtime_install_verifies_existing_files_and_writes_real_hashes(setup):
    archive = setup.parent / "engine.zip"
    with zipfile.ZipFile(archive, "w") as package:
        package.writestr("llama-server.exe", b"synthetic-engine")
        package.writestr("lib.dll", b"synthetic-library")
    destination = setup.parent / "engine"
    server, manifest, identities = runtime.unpack(archive, destination)
    assert identities["lib.dll"] == hashlib.sha256(b"synthetic-library").hexdigest()
    assert server.is_file() and manifest.is_file()
    runtime.unpack(archive, destination)
    (destination / "lib.dll").write_bytes(b"changed")
    with pytest.raises(ValueError, match="existing_engine_file_mismatch"):
        runtime.unpack(archive, destination)
