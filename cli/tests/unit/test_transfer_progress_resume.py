"""Unit tests for streaming transfer progress and resumable model/engine acquisition."""

from contextlib import contextmanager
import copy
import hashlib
import io
import json
import os
from pathlib import Path
import tarfile
from types import SimpleNamespace
import zipfile

import httpx
import pytest

from inferyard.application import engine_prepare as app_engine_prepare
from inferyard.application import engine_source as app_engine_source
from inferyard.application import model_source as app_model_source
from inferyard.cli.arguments import parser
from inferyard.cli.request import build_request
from inferyard.provisioning import (
    engine_prepare,
    engine_source_download,
    model_source_download,
    preparation_io,
    runtime_profiles,
    runtime_verification,
    windows_runtime_prepare,
)
from inferyard.provisioning.preparation_io import PreparationError, json_bytes
from inferyard.provisioning.runtime_archive import member_records
from inferyard.provisioning.runtime_profiles import PROFILES, output_files, profile_header
from inferyard.provisioning.transfer_progress import (
    ProgressEmitter,
    parse_content_length,
    parse_content_range,
)

REVISION = "499bc8821c6b12b4e53c5bffcb21ec206f212d81"
MODEL_URL = f"https://huggingface.co/ggml-org/models-moved/blob/{REVISION}/tinyllamas/stories260K.gguf"
ENGINE_URL = "https://example.test/llama-prism-b10743.tar.gz"
SECRET_TOKEN = "SECRET_RESUME_TOKEN_98765"


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


def _zip_bytes(entries):
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as pkg:
        for name, data in entries.items():
            pkg.writestr(name, data)
    return buf.getvalue()


def _parse_ndjson(text):
    return [json.loads(line) for line in text.splitlines() if line.strip()]


def test_parse_content_length_and_range():
    assert parse_content_length({"Content-Length": "1024"}) == 1024
    assert parse_content_length(httpx.Headers({"content-length": "42"})) == 42
    for bad in ({"Content-Length": "0"}, {"Content-Length": "-5"}, {"Content-Length": "abc"}, None):
        assert parse_content_length(bad) is None

    assert parse_content_range({"Content-Range": "bytes 10-19/20"}, 10) == (True, 20)
    assert parse_content_range({"Content-Range": "bytes 10-19/*"}, 10) == (True, None)
    assert parse_content_range({"Content-Range": "bytes 0-9/20"}, 10) == (False, None)
    assert parse_content_range({"Content-Range": "bytes 10-20/20"}, 10) == (False, None)
    assert parse_content_range({}, 10) == (False, None)


def test_progress_emitter_throttles_and_keeps_only_allowed_keys(monkeypatch):
    clock = iter([10.0, 10.05, 10.10, 10.35, 10.40, 10.41])
    monkeypatch.setattr("inferyard.provisioning.transfer_progress.time.monotonic", lambda: next(clock))

    disabled_buf = io.StringIO()
    ProgressEmitter(enabled=False, stream=disabled_buf).update("downloading", 10, 100)
    assert disabled_buf.getvalue() == ""

    buf = io.StringIO()
    emitter = ProgressEmitter(enabled=True, stream=buf, min_interval_seconds=0.25)
    for args in ((0, False), (25, False), (50, False), (75, False), (100, True)):
        emitter.update("downloading", args[0], 100, force=args[1])
    emitter.update("verifying", 100, 100)

    events = _parse_ndjson(buf.getvalue())
    assert [(e["stage"], e["downloaded_bytes"], e["total_bytes"]) for e in events] == [
        ("downloading", 0, 100),
        ("downloading", 75, 100),
        ("downloading", 100, 100),
        ("verifying", 100, 100),
    ]
    allowed = {"event", "stage", "downloaded_bytes", "total_bytes", "bytes_per_second"}
    assert all(set(item) == allowed and item["event"] == "transfer.progress" for item in events)
    assert events[0]["bytes_per_second"] is None and events[3]["bytes_per_second"] is None
    assert events[1]["bytes_per_second"] > 0 and events[2]["bytes_per_second"] > 0


def test_model_acquire_resume_206_and_progress_without_token_leak(monkeypatch, tmp_path, capsys):
    payload = b"0123456789abcdef" * 8
    split, digest = 37, hashlib.sha256(payload).hexdigest()
    monkeypatch.setenv("HF_RESUME_TEST", SECRET_TOKEN)
    out = tmp_path / "model-dir"

    fail_client = _ScriptedClient(
        lambda _u, _h: _ChunkedResponse(
            [payload[:20], payload[20:split]],
            headers={"Content-Length": str(len(payload)), "Content-Encoding": "identity"},
            fail_exc=httpx.ReadTimeout("https://huggingface.co/secret-timeout"),
        )
    )
    with pytest.raises(PreparationError, match="model_acquire_incomplete"):
        model_source_download.acquire(
            MODEL_URL, out, token_env="HF_RESUME_TEST", client=fail_client,
            progress_stream=True, resume=True,
        )
    part = out / "stories260K.gguf.part"
    assert part.read_bytes() == payload[:split]
    err1 = capsys.readouterr().err
    assert SECRET_TOKEN not in err1 and "huggingface.co" not in err1
    first_events = _parse_ndjson(err1)
    assert first_events[0]["stage"] == "downloading" and first_events[-1]["downloaded_bytes"] == split

    def resume_handler(_url, headers):
        assert headers.get("Range") == f"bytes={split}-"
        assert headers.get("Authorization") == f"Bearer {SECRET_TOKEN}"
        return _range_resp(payload, split, [payload[split : split + 30], payload[split + 30 :]])

    details = model_source_download.acquire(
        MODEL_URL, out, token_env="HF_RESUME_TEST", client=_ScriptedClient(resume_handler),
        progress_stream=True, resume=True,
    )
    assert details["bytes"] == len(payload) and details["sha256"] == digest
    assert (out / "stories260K.gguf").read_bytes() == payload and not part.exists()
    record = json.loads((out / "source.json").read_text(encoding="utf-8"))
    assert record["bytes"] == len(payload) and record["sha256"] == digest

    err2 = capsys.readouterr().err
    assert SECRET_TOKEN not in err2 and "huggingface.co" not in err2
    events = _parse_ndjson(err2)
    assert events[0]["stage"] == "downloading" and "verifying" in [e["stage"] for e in events]
    assert events[-1]["stage"] == "preparing" and events[-1]["downloaded_bytes"] == len(payload)


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


def test_model_acquire_resume_interrupt_and_bad_range(tmp_path):
    out = tmp_path / "kbd"
    client = _ScriptedClient(
        lambda _u, _h: _ChunkedResponse(
            [b"partial-bytes"], headers={"Content-Length": "100"}, fail_exc=KeyboardInterrupt()
        )
    )
    with pytest.raises(KeyboardInterrupt):
        model_source_download.acquire(MODEL_URL, out, client=client, resume=True)
    assert (out / "stories260K.gguf.part").read_bytes() == b"partial-bytes"

    bad = _ScriptedClient(
        lambda _u, _h: _ChunkedResponse(
            [b"x"], status_code=206, headers={"Content-Range": "bytes 0-10/100"}
        )
    )
    with pytest.raises(PreparationError, match="model_acquire_incomplete"):
        model_source_download.acquire(MODEL_URL, out, client=bad, resume=True)
    assert not (out / "stories260K.gguf.part").exists()


def test_engine_acquire_resume_and_progress(tmp_path, capsys):
    payload = b"engine-archive-bytes" * 4
    out = tmp_path / "eng"
    fail_client = _ScriptedClient(
        lambda _u, _h: _ChunkedResponse(
            [payload[:15]], headers={"Content-Length": str(len(payload))},
            fail_exc=httpx.ConnectError("offline"),
        )
    )
    with pytest.raises(PreparationError, match="engine_acquire_incomplete"):
        engine_source_download.acquire(ENGINE_URL, out, client=fail_client, resume=True)
    assert (out / "llama-prism-b10743.tar.gz.part").read_bytes() == payload[:15]

    ok_client = _ScriptedClient(
        lambda _u, h: _range_resp(payload, 15) if h.get("Range") == "bytes=15-" else _ChunkedResponse([], status_code=500)
    )
    details = engine_source_download.acquire(
        ENGINE_URL, out, client=ok_client, progress_stream=True, resume=True
    )
    assert details["bytes"] == len(payload) and details["sha256"] == hashlib.sha256(payload).hexdigest()
    assert (out / "llama-prism-b10743.tar.gz").read_bytes() == payload
    assert [e["stage"] for e in _parse_ndjson(capsys.readouterr().err)] == [
        "downloading", "downloading", "verifying", "preparing"
    ]


def test_engine_prepare_resume_progress_and_checksum_cleanup(tmp_path, capsys):
    archive = _tar_gz_bytes([("dist/llama-server", b"#!/bin/sh\necho ok\n", 0o755)])
    digest, split, out = hashlib.sha256(archive).hexdigest(), len(archive) // 2, tmp_path / "prepared"

    fail_client = _ScriptedClient(
        lambda _u, _h: _ChunkedResponse(
            [archive[:split]], headers={"Content-Length": str(len(archive))},
            fail_exc=httpx.ReadTimeout("timeout"),
        )
    )
    with pytest.raises(PreparationError, match="engine_acquire_incomplete"):
        engine_prepare.prepare(
            ENGINE_URL, out, expected_sha256=digest, client=fail_client, progress_stream=True, resume=True
        )
    assert (out / "llama-prism-b10743.tar.gz.part").read_bytes() == archive[:split]
    assert [p for p in tmp_path.iterdir() if p.name.startswith(".engine-prepare-")] == []
    capsys.readouterr()

    resume_client = _ScriptedClient(
        lambda _u, h: _range_resp(archive, split) if h.get("Range") == f"bytes={split}-" else _ChunkedResponse([], status_code=500)
    )
    details = engine_prepare.prepare(
        ENGINE_URL, out, expected_sha256=digest, client=resume_client, progress_stream=True, resume=True
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
        engine_prepare.prepare(ENGINE_URL, bad_out, expected_sha256="0" * 64, client=bad_client, resume=True)
    assert not bad_out.exists()


def test_engine_prepare_cuda_combined_progress_and_runtime_resume(tmp_path, monkeypatch, capsys):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Windows", "x64"))
    name = next(n for n, item in PROFILES.items() if item == "cuda")
    profile = profile_header(name)
    contents = {
        "engine": {"llama-server.exe": b"win-server", "ggml.dll": b"win-ggml"},
        "runtime": {"cuda/cudart64_12.dll": b"win-cudart-payload"},
    }
    archives, members = {}, []
    for role, files in contents.items():
        raw = _zip_bytes(files)
        zip_path = tmp_path / f"cuda-{role}.zip"
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
    for mod in (runtime_profiles, windows_runtime_prepare, runtime_verification):
        monkeypatch.setattr(mod, "load_profile", lambda _: (copy.deepcopy(profile), digest))
    ver = {"argv": ["--version"], "returncode": 0, "stdout": "10743 adfffbe41", "stderr": ""}
    for mod in (windows_runtime_prepare, runtime_verification):
        monkeypatch.setattr(mod, "query_version", lambda *a, **k: dict(ver))

    engine_url, engine_raw = archives["engine"]
    runtime_url, runtime_raw = archives["runtime"]
    rt_split, combined_total = len(runtime_raw) // 2, len(engine_raw) + len(runtime_raw)
    out = tmp_path / "win-cuda"
    eng_sha, rt_sha = hashlib.sha256(engine_raw).hexdigest(), hashlib.sha256(runtime_raw).hexdigest()

    def fail_runtime(url, _headers):
        if url == engine_url:
            return _ChunkedResponse([engine_raw], headers={"Content-Length": str(len(engine_raw))})
        return _ChunkedResponse(
            [runtime_raw[:rt_split]],
            headers={"Content-Length": str(len(runtime_raw))},
            fail_exc=httpx.ReadTimeout("rt-timeout"),
        )

    with pytest.raises(PreparationError, match="engine_acquire_incomplete"):
        engine_prepare.prepare(
            engine_url, out, expected_sha256=eng_sha, runtime_profile=name, runtime_url=runtime_url,
            runtime_expected_sha256=rt_sha, client=_ScriptedClient(fail_runtime),
            progress_stream=True, resume=True,
        )
    assert (out / "engine.zip.part").read_bytes() == engine_raw
    assert (out / "runtime.zip.part").read_bytes() == runtime_raw[:rt_split]
    capsys.readouterr()

    resume_calls = []

    def resume_runtime(url, headers):
        resume_calls.append((url, headers))
        if url == runtime_url and headers.get("Range") == f"bytes={rt_split}-":
            return _range_resp(runtime_raw, rt_split)
        raise AssertionError(f"unexpected request: {url} {headers}")

    details = engine_prepare.prepare(
        engine_url, out, expected_sha256=eng_sha, runtime_profile=name, runtime_url=runtime_url,
        runtime_expected_sha256=rt_sha, client=_ScriptedClient(resume_runtime),
        progress_stream=True, resume=True,
    )
    assert details["bytes"] == len(engine_raw) and len(resume_calls) == 1
    events = _parse_ndjson(capsys.readouterr().err)
    assert all(e["total_bytes"] == combined_total for e in events)
    assert events[-1]["stage"] == "preparing" and events[-1]["downloaded_bytes"] == combined_total


def test_cli_flags_and_application_forwarding(monkeypatch, tmp_path):
    p = parser()
    for argv, expected_cmd in [
        (["model", "acquire", "--source", MODEL_URL, "--out", str(tmp_path / "m")], "model acquire"),
        (["engine", "acquire", "--source", ENGINE_URL, "--out", str(tmp_path / "e")], "engine acquire"),
        (["engine", "prepare", "--source", ENGINE_URL, "--out", str(tmp_path / "p")], "engine prepare"),
    ]:
        req = build_request(p.parse_args([*argv, "--progress-stream", "--resume"]))
        assert (req.command, req.progress_stream, req.resume_transfer) == (expected_cmd, True, True)

    seen = {}
    monkeypatch.setattr(model_source_download, "acquire", lambda *a, **k: seen.setdefault("m", k) or {"bytes": 1})
    monkeypatch.setattr(engine_source_download, "acquire", lambda *a, **k: seen.setdefault("e", k) or {"bytes": 1})
    monkeypatch.setattr(engine_prepare, "prepare", lambda *a, **k: seen.setdefault("p", k) or {"bytes": 1})
    app_model_source.execute(
        SimpleNamespace(command="model acquire", model_source_url=MODEL_URL, out=tmp_path / "m",
                        model_token_env=None, model_timeout_seconds=10.0, progress_stream=True, resume_transfer=True)
    )
    app_engine_source.execute(
        SimpleNamespace(command="engine acquire", engine_source_url=ENGINE_URL, out=tmp_path / "e",
                        engine_timeout_seconds=10.0, progress_stream=True, resume_transfer=True)
    )
    app_engine_prepare.execute(
        SimpleNamespace(command="engine prepare", engine_source_url=ENGINE_URL, out=tmp_path / "p",
                        engine_timeout_seconds=10.0, progress_stream=True, resume_transfer=True)
    )
    assert all(seen[k]["progress_stream"] and seen[k]["resume"] for k in ("m", "e", "p"))
