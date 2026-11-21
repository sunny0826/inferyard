"""Unit tests for engine download streaming, safe preparation, and CLI command registration."""

import copy
import gzip
import hashlib
import io
import json
import os
import stat
import subprocess
import sys
import tarfile
import zipfile
from contextlib import contextmanager
from pathlib import Path
from types import SimpleNamespace

import httpx
import pytest

from inferyard.application.engine_prepare import execute
from inferyard.cli.arguments import parser
from inferyard.provisioning import (
    engine_prepare,
    engine_source_download,
    preparation_io,
    runtime_profiles,
    runtime_verification,
    windows_runtime_prepare,
)
from inferyard.provisioning.preparation_io import PreparationError, json_bytes
from inferyard.provisioning.runtime_archive import member_records
from inferyard.provisioning.runtime_profiles import PROFILES, output_files, profile_header

CLI_ROOT = Path(__file__).resolve().parents[2]


class _FakeResponse:
    def __init__(self, body, *, status_code=200, headers=None):
        self._body = body
        self.status_code = status_code
        self.headers = httpx.Headers(headers or {})

    def iter_raw(self):
        mid = len(self._body) // 2
        if mid:
            yield self._body[:mid]
        yield self._body[mid:]


class _FakeClient:
    def __init__(self, routes):
        self.routes = routes
        self.requests = []

    @contextmanager
    def stream(self, method, url, *, headers=None):
        self.requests.append((method, url, dict(headers or {})))
        item = self.routes[url]
        if isinstance(item, Exception):
            raise item
        yield item


def _tar_gz_bytes(entries):
    buf = io.BytesIO()
    with gzip.GzipFile(fileobj=buf, mode="wb", mtime=0) as gz:
        with tarfile.open(fileobj=gz, mode="w") as package:
            for entry in entries:
                kind, name, *rest = entry
                info = tarfile.TarInfo(name)
                if kind == "dir":
                    info.type, info.mode = tarfile.DIRTYPE, rest[0] if rest else 0o755
                    package.addfile(info)
                elif kind in ("symlink", "hardlink"):
                    info.type = tarfile.SYMTYPE if kind == "symlink" else tarfile.LNKTYPE
                    info.linkname = rest[0]
                    package.addfile(info)
                elif kind == "fifo":
                    info.type = tarfile.FIFOTYPE
                    package.addfile(info)
                else:
                    data = rest[0]
                    info.size = len(data)
                    info.mode = rest[1] if len(rest) > 1 else 0o644
                    package.addfile(info, io.BytesIO(data))
    return buf.getvalue()


def _zip_bytes(entries):
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as package:
        for name, spec in entries.items():
            if isinstance(spec, dict):
                info = zipfile.ZipInfo(name)
                info.external_attr = spec["attr"]
                package.writestr(info, spec["data"])
            else:
                package.writestr(name, spec)
    return buf.getvalue()


def _staging_dirs(parent):
    return [p for p in parent.iterdir() if p.name.startswith(".engine-prepare-")]


def test_download_to_stream_verifies_identity_status_and_sha256():
    payload = b"hello engine archive"
    digest = hashlib.sha256(payload).hexdigest()
    url = "https://example.test/engine.tar.gz"
    client = _FakeClient({url: _FakeResponse(payload)})
    out = io.BytesIO()

    size, got = engine_source_download.download_to_stream(
        client, url, out, expected_sha256=digest
    )
    assert (size, got) == (len(payload), digest)
    assert out.getvalue() == payload
    assert client.requests == [("GET", url, {"Accept-Encoding": "identity"})]

    with pytest.raises(PreparationError, match="engine_checksum_mismatch"):
        engine_source_download.download_to_stream(
            _FakeClient({url: _FakeResponse(payload)}), url, io.BytesIO(), expected_sha256="0" * 64
        )

    for bad in (
        _FakeResponse(payload, status_code=404),
        _FakeResponse(payload, headers={"Content-Encoding": "gzip"}),
    ):
        with pytest.raises(PreparationError, match="engine_acquire_incomplete"):
            engine_source_download.download_to_stream(_FakeClient({url: bad}), url, io.BytesIO())

    with pytest.raises(PreparationError, match="invalid_input"):
        engine_source_download.download_to_stream(
            client, url, io.BytesIO(), expected_sha256="not-a-sha256"
        )


def test_acquire_remains_backward_compatible(tmp_path):
    payload = b"raw-archive-bytes"
    digest = hashlib.sha256(payload).hexdigest()
    url = "https://example.test/llama.tar.gz"
    out = tmp_path / "acquired"
    details = engine_source_download.acquire(
        url, out, client=_FakeClient({url: _FakeResponse(payload)})
    )
    assert details["bytes"] == len(payload)
    assert details["sha256"] == digest
    assert Path(details["engine"]).read_bytes() == payload
    record = json.loads(Path(details["source_record"]).read_text(encoding="utf-8"))
    assert record["source_url"] == url and record["sha256"] == digest


@pytest.mark.parametrize("suffix", [".tar.gz", ".tgz"])
def test_prepare_tar_extracts_binary_and_safe_symlink(tmp_path, suffix):
    archive = _tar_gz_bytes(
        [
            ("dir", "./dist"),
            ("file", "./dist/llama-server", b"#!/bin/sh\n", 0o644),
            ("file", "./dist/libimpl.0.dylib", b"lib", 0o644),
            ("symlink", "./dist/libimpl.dylib", "libimpl.0.dylib"),
        ]
    )
    digest = hashlib.sha256(archive).hexdigest()
    url = f"https://example.test/llama{suffix}"
    out = tmp_path / "prepared"

    details = engine_prepare.prepare(
        url, out, expected_sha256=digest, client=_FakeClient({url: _FakeResponse(archive)})
    )

    engine = Path(details["engine"])
    assert engine == out.resolve() / "dist" / "llama-server"
    assert engine.read_bytes() == b"#!/bin/sh\n"
    if os.name != "nt":
        assert engine.stat().st_mode & 0o777 == 0o755
    link = out / "dist" / "libimpl.dylib"
    assert link.is_symlink() and link.read_bytes() == b"lib"
    assert details["bytes"] == len(archive) and details["sha256"] == digest
    assert details["runtime_receipt"] is None
    assert details["ready_to_run"] is False and details["model_requests_sent"] == 0
    record = json.loads(Path(details["source_record"]).read_text(encoding="utf-8"))
    assert record["source_url"] == url and record["engine"] == details["engine"]
    assert _staging_dirs(tmp_path) == []


def test_prepare_zip_and_standalone_binary(tmp_path):
    zip_payload = _zip_bytes({"bin/llama-server": b"zip-bin", "bin/libggml.dylib": b"lib"})
    zip_url = "https://example.test/engine.zip"
    zip_out = tmp_path / "zip-out"
    zip_details = engine_prepare.prepare(
        zip_url,
        zip_out,
        expected_sha256=hashlib.sha256(zip_payload).hexdigest(),
        client=_FakeClient({zip_url: _FakeResponse(zip_payload)}),
    )
    assert Path(zip_details["engine"]) == zip_out.resolve() / "bin" / "llama-server"
    if os.name != "nt":
        assert Path(zip_details["engine"]).stat().st_mode & 0o777 == 0o755

    bin_payload = b"standalone-server"
    bin_url = "https://example.test/llama-server"
    bin_out = tmp_path / "bin-out"
    bin_details = engine_prepare.prepare(
        bin_url,
        bin_out,
        expected_sha256=hashlib.sha256(bin_payload).hexdigest(),
        client=_FakeClient({bin_url: _FakeResponse(bin_payload)}),
    )
    assert Path(bin_details["engine"]) == bin_out.resolve() / "llama-server"
    assert Path(bin_details["engine"]).read_bytes() == bin_payload
    if os.name != "nt":
        assert Path(bin_details["engine"]).stat().st_mode & 0o777 == 0o755
    assert _staging_dirs(tmp_path) == []


@pytest.mark.parametrize(
    ("url", "payload", "expected_reason"),
    [
        ("https://example.test/not-engine.bin", b"x", "engine_binary_missing"),
        (
            "https://example.test/no-bin.tar.gz",
            _tar_gz_bytes([("file", "README.md", b"hi")]),
            "engine_binary_missing",
        ),
        (
            "https://example.test/no-bin.zip",
            _zip_bytes({"README.md": b"hi"}),
            "engine_binary_missing",
        ),
        ("https://example.test/corrupt.tar.gz", b"not-gzip", "engine_unpack_failed"),
        ("https://example.test/corrupt.zip", b"not-zip", "engine_unpack_failed"),
        (
            "https://example.test/traversal.tar.gz",
            _tar_gz_bytes([("file", "../evil", b"x")]),
            "unsafe_archive_entry",
        ),
        (
            "https://example.test/abs.tar.gz",
            _tar_gz_bytes([("file", "/etc/passwd", b"x")]),
            "unsafe_archive_entry",
        ),
        (
            "https://example.test/abs-link.tar.gz",
            _tar_gz_bytes([("file", "llama-server", b"bin"), ("symlink", "bad", "/etc/passwd")]),
            "unsafe_archive_entry",
        ),
        (
            "https://example.test/escape-link.tar.gz",
            _tar_gz_bytes(
                [("file", "llama-server", b"bin"), ("symlink", "sub/bad", "../../outside")]
            ),
            "unsafe_archive_entry",
        ),
        (
            "https://example.test/chained-escape.tar.gz",
            _tar_gz_bytes(
                [
                    ("file", "llama-server", b"bin"),
                    ("symlink", "sub/a", ".."),
                    ("symlink", "sub/b", "a/.."),
                ]
            ),
            "unsafe_archive_entry",
        ),
        (
            "https://example.test/loop-link.tar.gz",
            _tar_gz_bytes(
                [("file", "llama-server", b"bin"), ("symlink", "a", "b"), ("symlink", "b", "a")]
            ),
            "unsafe_archive_entry",
        ),
        (
            "https://example.test/write-through-link.tar.gz",
            _tar_gz_bytes(
                [
                    ("file", "llama-server", b"bin"),
                    ("symlink", "sub", "inner"),
                    ("file", "sub/evil", b"x"),
                ]
            ),
            "unsafe_archive_entry",
        ),
        (
            "https://example.test/hardlink-escape.tar.gz",
            _tar_gz_bytes(
                [("file", "llama-server", b"bin"), ("hardlink", "bad", "../../etc/passwd")]
            ),
            "unsafe_archive_entry",
        ),
        ("https://example.test/fifo.tar.gz", _tar_gz_bytes([("fifo", "pipe")]), "unsafe_archive_entry"),
        ("https://example.test/traversal.zip", _zip_bytes({"../evil": b"x"}), "unsafe_archive_entry"),
        (
            "https://example.test/symlink.zip",
            _zip_bytes({"bad": {"data": b"/etc/passwd", "attr": (stat.S_IFLNK | 0o777) << 16}}),
            "unsafe_archive_entry",
        ),
    ],
)
def test_prepare_rejects_invalid_or_unsafe_archives(tmp_path, url, payload, expected_reason):
    out = tmp_path / "out"
    with pytest.raises(PreparationError, match=expected_reason):
        engine_prepare.prepare(url, out, client=_FakeClient({url: _FakeResponse(payload)}))
    assert not out.exists()
    assert _staging_dirs(tmp_path) == []


def test_prepare_rejects_invalid_urls_existing_output_and_http_errors(tmp_path):
    with pytest.raises(PreparationError) as exc_info:
        engine_prepare.prepare(
            "https://example.test/engine.tar.gz?token=SECRET", tmp_path / "out"
        )
    assert exc_info.value.reason == "invalid_engine_source"
    assert "SECRET" not in str(exc_info.value)

    existing = tmp_path / "existing"
    existing.mkdir()
    with pytest.raises(PreparationError, match="output_exists"):
        engine_prepare.prepare("https://example.test/engine.tar.gz", existing)

    url = "https://example.test/engine.tar.gz"
    with pytest.raises(PreparationError, match="engine_acquire_incomplete"):
        engine_prepare.prepare(
            url, tmp_path / "net-fail", client=_FakeClient({url: httpx.ConnectError("offline")})
        )
    assert _staging_dirs(tmp_path) == []


def _synthetic_profile_fixture(tmp_path, monkeypatch, mode):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Windows", "x64"))
    name = next(n for n, item in PROFILES.items() if item == mode)
    profile = profile_header(name)
    contents = {"engine": {"llama-server.exe": b"fake win server", "ggml.dll": b"fake ggml"}}
    if mode == "cuda":
        contents["runtime"] = {"cuda/sub/cudart64_12.dll": b"fake cudart"}
    archives, members = {}, []
    for role, files in contents.items():
        raw = _zip_bytes(files)
        zip_path = tmp_path / f"fixture-{role}.zip"
        zip_path.write_bytes(raw)
        with zipfile.ZipFile(zip_path) as package:
            members.extend(member_records(package, role))
        for entry in profile["archives"]:
            if entry["role"] == role:
                entry["bytes"], entry["sha256"] = len(raw), hashlib.sha256(raw).hexdigest()
        archives[role] = (f"https://example.test/{role}.zip", raw)
    profile["members"] = members
    server, manifest, libraries, _ = output_files(members)
    profile.update(engine_binary=server, runtime_library_manifest=manifest, libraries=libraries)
    digest = hashlib.sha256(json_bytes(profile)).hexdigest()
    for module in (runtime_profiles, windows_runtime_prepare, runtime_verification):
        monkeypatch.setattr(module, "load_profile", lambda _: (copy.deepcopy(profile), digest))
    version_ok = {
        "argv": ["--version"],
        "returncode": 0,
        "stdout": "10743 adfffbe41",
        "stderr": "",
    }
    for module in (windows_runtime_prepare, runtime_verification):
        monkeypatch.setattr(module, "query_version", lambda *a, **k: dict(version_ok))
    return name, archives


@pytest.mark.parametrize("mode", ["cpu", "cuda"])
def test_prepare_windows_runtime_profiles(tmp_path, monkeypatch, mode):
    name, archives = _synthetic_profile_fixture(tmp_path, monkeypatch, mode)
    routes = {url: _FakeResponse(raw) for url, raw in archives.values()}
    engine_url, engine_raw = archives["engine"]
    runtime_url, runtime_raw = archives.get("runtime", (None, None))
    out = tmp_path / f"win-{mode}"

    details = engine_prepare.prepare(
        engine_url,
        out,
        expected_sha256=hashlib.sha256(engine_raw).hexdigest(),
        runtime_profile=name,
        runtime_url=runtime_url,
        runtime_expected_sha256=hashlib.sha256(runtime_raw).hexdigest() if runtime_raw else None,
        client=_FakeClient(routes),
    )

    assert details["profile"] == name and details["mode"] == mode
    assert details["bytes"] == len(engine_raw)
    assert details["sha256"] == hashlib.sha256(engine_raw).hexdigest()
    assert details["runtime_receipt"] == str(out.resolve() / "runtime-receipt.json")
    assert details["source_record"] is None and not (out / "engine-prepare.json").exists()
    assert Path(details["runtime_library_manifest"]).exists()
    if mode == "cuda":
        assert (Path(details["engine"]).parent / "cudart64_12.dll").exists()
    assert _staging_dirs(tmp_path) == []
    runtime_verification.verify_receipt(Path(details["runtime_receipt"]), mode=mode)


def test_prepare_windows_profile_guards(tmp_path, monkeypatch):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Darwin", "arm64"))
    with pytest.raises(PreparationError, match="unsupported_platform"):
        engine_prepare.prepare(
            "https://example.test/win.zip", tmp_path / "mac", runtime_profile=next(iter(PROFILES))
        )

    cuda_name, cuda_archives = _synthetic_profile_fixture(tmp_path, monkeypatch, "cuda")
    engine_url, engine_raw = cuda_archives["engine"]
    runtime_url, _ = cuda_archives["runtime"]
    with pytest.raises(PreparationError, match="runtime_archive_required"):
        engine_prepare.prepare(engine_url, tmp_path / "missing-rt", runtime_profile=cuda_name)
    with pytest.raises(PreparationError, match="engine_checksum_mismatch"):
        engine_prepare.prepare(
            engine_url,
            tmp_path / "bad-sha",
            expected_sha256="0" * 64,
            runtime_profile=cuda_name,
            runtime_url=runtime_url,
        )
    with pytest.raises(PreparationError, match="engine_checksum_mismatch"):
        engine_prepare.prepare(
            engine_url,
            tmp_path / "bad-rt-sha",
            expected_sha256=hashlib.sha256(engine_raw).hexdigest(),
            runtime_profile=cuda_name,
            runtime_url=runtime_url,
            runtime_expected_sha256="0" * 64,
        )

    cpu_name, cpu_archives = _synthetic_profile_fixture(tmp_path, monkeypatch, "cpu")
    with pytest.raises(PreparationError, match="runtime_archive_forbidden"):
        engine_prepare.prepare(
            cpu_archives["engine"][0],
            tmp_path / "extra-rt",
            runtime_profile=cpu_name,
            runtime_url="https://example.test/cudart.zip",
        )


def test_application_engine_prepare_execute_and_errors(tmp_path, monkeypatch):
    expected = {
        "out": str(tmp_path / "out"),
        "engine": str(tmp_path / "out" / "llama-server"),
        "source_record": str(tmp_path / "out" / "engine-prepare.json"),
        "bytes": 10,
        "sha256": "a" * 64,
        "runtime_receipt": None,
        "ready_to_run": False,
        "model_requests_sent": 0,
    }
    monkeypatch.setattr(engine_prepare, "prepare", lambda *a, **k: expected)
    req = SimpleNamespace(
        command="engine prepare",
        engine_source_url="https://example.test/engine.tar.gz",
        out=tmp_path / "out",
        expected_sha256="a" * 64,
        runtime_profile=None,
        runtime_source_url=None,
        runtime_expected_sha256=None,
        engine_timeout_seconds=120.0,
    )
    code, result = execute(req)
    assert code == 0 and result.command == "engine prepare"
    assert result.status == "prepared" and result.completeness == "complete"
    assert result.details == expected

    for err_code, want_status in ((2, "blocked"), (4, "error"), (130, "interrupted")):
        def fail(*a, _code=err_code, **k):
            raise PreparationError("engine_checksum_mismatch", _code)

        monkeypatch.setattr(engine_prepare, "prepare", fail)
        code, result = execute(req)
        assert (code, result.status, result.limitations) == (
            err_code,
            want_status,
            ("engine_checksum_mismatch",),
        )


def test_cli_engine_prepare_parser_and_cold_help(tmp_path):
    args = parser().parse_args(
        [
            "engine",
            "prepare",
            "--source",
            "https://example.test/engine.zip",
            "--out",
            str(tmp_path / "out"),
            "--expected-sha256",
            "a" * 64,
            "--profile",
            "prism-b10743-adfffbe-win-cuda-12.4-x64",
            "--runtime-source",
            "https://example.test/cudart.zip",
            "--runtime-expected-sha256",
            "b" * 64,
            "--timeout-seconds",
            "45",
        ]
    )
    assert args.command == "engine prepare"
    assert args.engine_source_url == "https://example.test/engine.zip"
    assert args.out == tmp_path / "out"
    assert args.expected_sha256 == "a" * 64
    assert args.runtime_profile == "prism-b10743-adfffbe-win-cuda-12.4-x64"
    assert args.runtime_source_url == "https://example.test/cudart.zip"
    assert args.runtime_expected_sha256 == "b" * 64
    assert args.engine_timeout_seconds == 45.0

    script = """
import contextlib, importlib.abc, io, sys
sys.path.insert(0, sys.argv[1])
blocked = ("inferyard.runtime", "inferyard.extensions", "inferyard.adapters", "httpx")
class Reject(importlib.abc.MetaPathFinder):
    def find_spec(self, name, path=None, target=None):
        if any(name == b or name.startswith(b + ".") for b in blocked):
            raise AssertionError("imported " + name)
sys.meta_path.insert(0, Reject())
from inferyard.cli import main
out, err = io.StringIO(), io.StringIO()
try:
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        main(["engine", "prepare", "--help"])
except SystemExit as exc:
    assert exc.code == 0 and "--expected-sha256" in out.getvalue() and not err.getvalue()
else:
    raise AssertionError("expected SystemExit")
"""
    proc = subprocess.run(
        [sys.executable, "-c", script, str(CLI_ROOT)],
        capture_output=True,
        text=True,
        check=False,
        env={**os.environ, "NO_COLOR": "1", "PYTHON_COLORS": "0"},
    )
    assert proc.returncode == 0, (proc.stdout, proc.stderr)
