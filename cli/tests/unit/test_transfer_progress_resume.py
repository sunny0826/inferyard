"""Unit tests for safe overwrite/symlink guards and checksum cleanup during transfer."""

import hashlib
import io
import json
import os
import tarfile
from contextlib import contextmanager
from pathlib import Path

import httpx
import pytest

from inferyard.provisioning import engine_prepare, model_source_download
from inferyard.provisioning.preparation_io import PreparationError

REVISION = "499bc8821c6b12b4e53c5bffcb21ec206f212d81"
MODEL_URL = f"https://huggingface.co/ggml-org/models-moved/blob/{REVISION}/tinyllamas/stories260K.gguf"
ENGINE_URL = "https://example.test/llama-prism-b10743.tar.gz"


class _ChunkedResponse:
    def __init__(self, chunks, *, status_code=200, headers=None, fail_exc=None):
        self._chunks = chunks
        self.status_code = status_code
        self.headers = httpx.Headers(headers or {})
        self._fail_exc = fail_exc

    def iter_raw(self):
        yield from self._chunks
        if self._fail_exc is not None:
            raise self._fail_exc


class _ScriptedClient:
    def __init__(self, handler):
        self._handler = handler
        self.requests = []

    @contextmanager
    def stream(self, method, url, *, headers=None):
        hdrs = dict(headers or {})
        self.requests.append((method, url, hdrs))
        resp = self._handler(url, hdrs)
        if isinstance(resp, BaseException):
            raise resp
        yield resp


def _range_resp(payload: bytes, offset: int, chunks=None):
    return _ChunkedResponse(
        chunks or [payload[offset:]],
        status_code=206,
        headers={
            "Content-Range": f"bytes {offset}-{len(payload) - 1}/{len(payload)}",
            "Content-Length": str(len(payload) - offset),
            "Content-Encoding": "identity",
        },
    )


def _tar_gz_bytes(entries):
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as pkg:
        for name, data, mode in entries:
            info = tarfile.TarInfo(name)
            info.size, info.mode = len(data), mode
            pkg.addfile(info, io.BytesIO(data))
    return buf.getvalue()


def _parse_ndjson(text):
    return [json.loads(line) for line in text.splitlines() if line.strip()]


def test_model_acquire_resume_200_truncates_and_guards_symlinks(tmp_path):
    payload = b"full-fresh-gguf-payload"
    out = tmp_path / "m"
    out.mkdir()
    (out / "stories260K.gguf.part").write_bytes(b"stale-longer-partial-data-from-before")

    client = _ScriptedClient(
        lambda _u, _h: _ChunkedResponse(
            [payload[:8], payload[8:]],
            headers={"Content-Length": str(len(payload)), "Content-Encoding": "identity"},
        )
    )
    details = model_source_download.acquire(MODEL_URL, out, client=client, resume=True)
    assert details["bytes"] == len(payload) and details["sha256"] == hashlib.sha256(payload).hexdigest()
    assert (out / "stories260K.gguf").read_bytes() == payload

    with pytest.raises(PreparationError, match="output_exists"):
        model_source_download.acquire(MODEL_URL, out, client=client, resume=True)

    if os.name != "nt":
        real, link = tmp_path / "real", tmp_path / "link"
        real.mkdir()
        link.symlink_to(real)
        with pytest.raises(PreparationError, match="output_exists"):
            model_source_download.acquire(MODEL_URL, link, client=client, resume=True)


def test_engine_prepare_resume_progress_and_checksum_cleanup(tmp_path, capsys):
    archive = _tar_gz_bytes([("dist/llama-server", b"#!/bin/sh\necho ok\n", 0o755)])
    digest, split, out = hashlib.sha256(archive).hexdigest(), len(archive) // 2, tmp_path / "prepared"

    fail_client = _ScriptedClient(
        lambda _u, _h: _ChunkedResponse(
            [archive[:split]],
            headers={"Content-Length": str(len(archive))},
            fail_exc=httpx.ReadTimeout("timeout"),
        )
    )
    with pytest.raises(PreparationError, match="engine_acquire_incomplete"):
        engine_prepare.prepare(
            ENGINE_URL,
            out,
            expected_sha256=digest,
            client=fail_client,
            progress_stream=True,
            resume=True,
        )
    assert (out / "llama-prism-b10743.tar.gz.part").read_bytes() == archive[:split]
    assert [p for p in tmp_path.iterdir() if p.name.startswith(".engine-prepare-")] == []
    capsys.readouterr()

    resume_client = _ScriptedClient(
        lambda _u, h: _range_resp(archive, split)
        if h.get("Range") == f"bytes={split}-"
        else _ChunkedResponse([], status_code=500)
    )
    details = engine_prepare.prepare(
        ENGINE_URL,
        out,
        expected_sha256=digest,
        client=resume_client,
        progress_stream=True,
        resume=True,
    )
    assert Path(details["engine"]).read_bytes() == b"#!/bin/sh\necho ok\n"
    assert [p for p in tmp_path.iterdir() if p.name.startswith(".engine-prepare-")] == []
    stages = {e["stage"] for e in _parse_ndjson(capsys.readouterr().err)}
    assert {"downloading", "verifying", "extracting", "preparing"} <= stages

    bad_out = tmp_path / "bad-checksum"
    bad_client = _ScriptedClient(
        lambda _u, _h: _ChunkedResponse([archive], headers={"Content-Length": str(len(archive))})
    )
    with pytest.raises(PreparationError, match="engine_checksum_mismatch"):
        engine_prepare.prepare(
            ENGINE_URL, bad_out, expected_sha256="0" * 64, client=bad_client, resume=True
        )
    assert not bad_out.exists()
