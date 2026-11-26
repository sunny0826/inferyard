"""Security boundary tests for engine archive unpacking and URL sanitization."""

import gzip
import io
import stat
import tarfile
import zipfile
from contextlib import contextmanager

import httpx
import pytest

from inferyard.provisioning import engine_prepare
from inferyard.provisioning.preparation_io import PreparationError


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

    @contextmanager
    def stream(self, method, url, *, headers=None):
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
                package.writestr(zipfile.ZipInfo(name), spec)
    return buf.getvalue()


def _staging_dirs(parent):
    return [p for p in parent.iterdir() if p.name.startswith(".engine-prepare-")]


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
        (
            "https://example.test/fifo.tar.gz",
            _tar_gz_bytes([("fifo", "pipe")]),
            "unsafe_archive_entry",
        ),
        (
            "https://example.test/traversal.zip",
            _zip_bytes({"../evil": b"x"}),
            "unsafe_archive_entry",
        ),
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
