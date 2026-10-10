"""Bounded model transfer and disk failures using only in-process fixtures."""

import hashlib
import json
from pathlib import Path

import httpx
import pytest

from inferyard.config.preparation_io import PreparationError
from tests.unit.test_model_source import FIXTURES, source

PAYLOAD = b"GGUF synthetic fixture, never a real model"


class Chunks(httpx.SyncByteStream):
    def __init__(self, chunks):
        self.chunks = chunks

    def __iter__(self):
        if getattr(self, "closed", False):
            raise httpx.StreamConsumed
        yield from self.chunks

    def close(self):
        self.closed = True


def fixture_client(
    *, payload=PAYLOAD, redirect=None, second_redirect=None, status=200, metadata_redirect=None
):
    seen = []
    data = json.loads((FIXTURES / "huggingface.json").read_bytes())
    file = next(f for f in data["siblings"] if f["rfilename"] == source("huggingface").path)
    file["size"] = len(PAYLOAD)
    file["lfs"].update(size=len(PAYLOAD), sha256=hashlib.sha256(PAYLOAD).hexdigest())

    def respond(request):
        seen.append(request)
        if request.url.path.startswith("/api/models/"):
            if metadata_redirect:
                return httpx.Response(302, headers={"location": metadata_redirect})
            return httpx.Response(200, stream=Chunks([json.dumps(data).encode()]))
        if redirect and len(seen) == 2:
            return httpx.Response(302, headers={"location": redirect})
        if second_redirect:
            return httpx.Response(302, headers={"location": second_redirect})
        chunks = [payload[:10], payload[10:]]
        return httpx.Response(status, stream=Chunks(chunks))

    return httpx.Client(transport=httpx.MockTransport(respond), trust_env=False), seen


def download(tmp_path, client, **kwargs):
    from inferyard.platforms.model_source_download import transfer
    from inferyard.platforms.model_source_host import Budget

    return transfer(
        source("huggingface"), tmp_path / "new", None, Budget(), client=client, **kwargs
    )


def test_streamed_file_and_source_are_published_after_fsync(tmp_path):
    client, _ = fixture_client()
    result = download(tmp_path, client)
    assert Path(result["model"]).read_bytes() == PAYLOAD
    record = json.loads(Path(result["source_record"]).read_bytes())
    assert record["bytes"] == len(PAYLOAD)
    assert record["sha256"] == hashlib.sha256(PAYLOAD).hexdigest()
    assert result["ready_to_run"] is False
    assert not list((tmp_path / "new").glob("*.part"))


@pytest.mark.parametrize("payload", [PAYLOAD[:-1], b"x" * len(PAYLOAD), PAYLOAD + b"x"])
def test_length_or_hash_mismatch_never_publishes_file(tmp_path, payload):
    client, _ = fixture_client(payload=payload)
    with pytest.raises(PreparationError, match="model_source_metadata_mismatch"):
        download(tmp_path, client)
    assert (tmp_path / "new").is_dir()
    assert list((tmp_path / "new").iterdir()) == []


def test_existing_output_is_rejected_before_any_request(tmp_path):
    (tmp_path / "new").mkdir()
    (tmp_path / "new/keep").write_text("original")
    client, seen = fixture_client()
    with pytest.raises(PreparationError, match="output_exists"):
        download(tmp_path, client)
    assert not seen
    assert (tmp_path / "new/keep").read_text() == "original"


@pytest.mark.parametrize(
    "redirect",
    [
        "https://evil.test/model.gguf",
        "http://cdn.hf.co/model.gguf",
        "https://hf.co/model.gguf",
        "https://hf.co.evil.test/model.gguf",
        "https://{{EMAIL_xa4y4s5y}}/model.gguf",
        "https://cdn.hf.co/model.gguf#secret",
        "https://cdn.hf.co/model.gguf?callback=https://evil.test",
        "https://huggingface.co/model.gguf?token=secret",
    ],
)
def test_unsafe_download_redirects_are_not_requested(tmp_path, redirect):
    client, seen = fixture_client(redirect=redirect)
    with pytest.raises(PreparationError, match="model_acquire_incomplete"):
        download(tmp_path, client)
    assert len(seen) == 2


def test_cdn_signed_query_is_kept_but_authorization_is_not(tmp_path, monkeypatch):
    monkeypatch.setenv("TEST_MODEL_TOKEN", "secret")
    target = (
        "https://us.aws.cdn.hf.co/model.gguf?Expires=123&Policy=p"
        "&Signature=s&Key-Pair-Id=k&Hash-Algorithm=SHA256&xip=public"
    )
    client, seen = fixture_client(redirect=target)
    from inferyard.platforms.model_source_download import transfer
    from inferyard.platforms.model_source_host import Budget

    result = transfer(
        source("huggingface"), tmp_path / "new", "TEST_MODEL_TOKEN", Budget(), client=client
    )
    assert all(r.headers.get("Authorization") == "Bearer secret" for r in seen[:2])
    assert "authorization" not in seen[2].headers
    assert str(seen[2].url) == target
    assert "secret" not in Path(result["source_record"]).read_text()


def test_one_real_redirect_response_is_closed_once(tmp_path):
    import time

    from inferyard.platforms.model_source_host import Budget
    from inferyard.platforms.model_source_http import response

    def handle(request):
        if request.url.host == "huggingface.co":
            return httpx.Response(
                302, headers={"location": "https://cdn.hf.co/file?Expires=1&Signature=abc"}
            )
        return httpx.Response(200, stream=httpx.ByteStream(PAYLOAD))

    client = httpx.Client(transport=httpx.MockTransport(handle), follow_redirects=False)
    with response(
        client,
        source("huggingface"),
        source("huggingface").download_url,
        None,
        Budget(total_seconds=5, read_seconds=5, max_bytes=100),
        time.monotonic() + 5,
    ) as opened:
        assert b"".join(opened.iter_raw()) == PAYLOAD


def test_second_redirect_is_rejected(tmp_path):
    client, seen = fixture_client(
        redirect="https://cdn.hf.co/model.gguf", second_redirect="https://other.hf.co/model.gguf"
    )
    with pytest.raises(PreparationError, match="model_acquire_incomplete"):
        download(tmp_path, client)
    assert len(seen) == 3


def test_metadata_cross_host_redirect_is_rejected(tmp_path):
    client, seen = fixture_client(metadata_redirect="https://cdn.hf.co/metadata")
    with pytest.raises(PreparationError, match="model_acquire_incomplete"):
        download(tmp_path, client)
    assert len(seen) == 1
    assert not (tmp_path / "new").exists()


def test_http_rejection_is_remote_failure(tmp_path):
    client, _ = fixture_client(status=403)
    with pytest.raises(PreparationError, match="model_acquire_incomplete") as caught:
        download(tmp_path, client)
    assert caught.value.code == 2


@pytest.mark.parametrize("operation", ["write", "flush", "fsync", "close"])
def test_local_disk_failures_are_io_errors_and_leave_failed_directory(
    tmp_path, monkeypatch, operation
):
    from contextlib import contextmanager

    from inferyard.config.preparation_io import NewDirectory
    from inferyard.platforms import model_source_download as module

    client, _ = fixture_client()
    original = NewDirectory.stream

    class Broken:
        def __init__(self, stream):
            self.stream = stream

        def write(self, data):
            if operation == "write":
                return len(data) - 1
            return self.stream.write(data)

    @contextmanager
    def stream(self, relative):
        with original(self, relative) as opened:
            yield Broken(opened)
            if operation in ("flush", "close"):
                raise OSError("SECRET disk exception")

    monkeypatch.setattr(NewDirectory, "stream", stream)
    if operation == "fsync":
        monkeypatch.setattr(module.os, "fsync", lambda _: (_ for _ in ()).throw(OSError("SECRET")))
    with pytest.raises(PreparationError, match="^io_error$") as caught:
        download(tmp_path, client)
    assert caught.value.code == 4
    assert (tmp_path / "new").is_dir()
    assert not (tmp_path / "new/source.json").exists()


def test_storage_budget_is_checked_before_file_request(tmp_path, monkeypatch):
    from types import SimpleNamespace

    from inferyard.platforms import model_source_download as module

    client, seen = fixture_client()
    monkeypatch.setattr(module.shutil, "disk_usage", lambda _: SimpleNamespace(free=len(PAYLOAD)))
    with pytest.raises(PreparationError, match="io_error") as caught:
        download(tmp_path, client)
    assert caught.value.code == 4
    assert len(seen) == 1
    assert not (tmp_path / "new").exists()


def test_download_cap_is_checked_before_file_request(tmp_path):
    from inferyard.platforms.model_source_download import transfer
    from inferyard.platforms.model_source_host import Budget

    client, seen = fixture_client()
    with pytest.raises(PreparationError, match="model_acquire_incomplete"):
        transfer(
            source("huggingface"),
            tmp_path / "new",
            None,
            Budget(max_bytes=len(PAYLOAD) - 1),
            client=client,
        )
    assert len(seen) == 1


def test_remote_response_interruption_is_sanitized(tmp_path):
    client, _ = fixture_client()
    original = client.send

    class Interrupted(httpx.SyncByteStream):
        def __iter__(self):
            yield PAYLOAD[:8]
            raise httpx.ReadError("SECRET response")

    def send(request, **kwargs):
        if "/resolve/" in request.url.path:
            return httpx.Response(200, stream=Interrupted(), request=request)
        return original(request, **kwargs)

    client.send = send
    with pytest.raises(PreparationError, match="^model_acquire_incomplete$"):
        download(tmp_path, client)
    assert not (tmp_path / "new/source.json").exists()


def test_existing_final_file_is_never_replaced(tmp_path, monkeypatch):
    from inferyard.platforms import model_source_download as module

    original = module.publish

    def raced(output, temporary, final):
        (output.path / final).write_bytes(b"original")
        original(output, temporary, final)

    monkeypatch.setattr(module, "publish", raced)
    client, _ = fixture_client()
    with pytest.raises(PreparationError, match="output_exists"):
        download(tmp_path, client)
    assert (tmp_path / "new/stories260K.gguf").read_bytes() == b"original"


def test_fsync_and_close_complete_before_atomic_publication(tmp_path, monkeypatch):
    from inferyard.platforms import model_source_download as module

    original_sync, original_publish = module.os.fsync, module.publish
    synchronized = []

    def sync(fd):
        synchronized.append(fd)
        original_sync(fd)

    def publish(output, temporary, final):
        assert synchronized
        assert not (output.path / final).exists()
        assert (output.path / temporary).stat().st_size > 0
        original_publish(output, temporary, final)

    monkeypatch.setattr(module.os, "fsync", sync)
    monkeypatch.setattr(module, "publish", publish)
    client, _ = fixture_client()
    download(tmp_path, client)


def test_modelscope_fixed_metadata_and_signed_cdn_transfer(tmp_path, monkeypatch):
    from inferyard.platforms.model_source_download import transfer
    from inferyard.platforms.model_source_host import Budget

    metadata = json.loads((FIXTURES / "modelscope.json").read_bytes())
    selected = source("modelscope")
    chosen = next(f for f in metadata["Data"]["Files"] if f["Path"] == selected.path)
    chosen.update(Size=len(PAYLOAD), Sha256=hashlib.sha256(PAYLOAD).hexdigest())
    seen = []
    monkeypatch.setenv("TEST_MS_TOKEN", "secret")

    def respond(request):
        seen.append(request)
        if len(seen) == 1:
            assert str(request.url) == selected.metadata_url
            return httpx.Response(200, stream=Chunks([json.dumps(metadata).encode()]))
        if len(seen) == 2:
            assert str(request.url) == selected.download_url
            return httpx.Response(
                302,
                headers={
                    "location": "https://cdn.modelscope.cn/file.gguf?Expires=1&OSSAccessKeyId=k&Signature=s"
                },
            )
        assert "authorization" not in request.headers
        return httpx.Response(200, stream=Chunks([PAYLOAD]))

    client = httpx.Client(transport=httpx.MockTransport(respond), trust_env=False)
    details = transfer(selected, tmp_path / "new", "TEST_MS_TOKEN", Budget(), client=client)
    assert len(seen) == 3
    assert Path(details["model"]).read_bytes() == PAYLOAD
    record = json.loads(Path(details["source_record"]).read_bytes())
    assert record["platform"] == "modelscope"
    assert record["revision"] == selected.revision


def test_unbounded_remote_stream_stops_at_declared_size(tmp_path):
    from inferyard.platforms.model_source_download import transfer
    from inferyard.platforms.model_source_host import Budget

    selected = source("huggingface")
    metadata = json.loads((FIXTURES / "huggingface.json").read_bytes())
    chosen = next(f for f in metadata["siblings"] if f["rfilename"] == selected.path)
    chosen["size"] = 1024 * 1024
    chosen["lfs"].update(size=chosen["size"], sha256="a" * 64)
    consumed = []

    class Endless(httpx.SyncByteStream):
        def __iter__(self):
            for index in range(1000):
                consumed.append(index)
                yield b"x" * (1024 * 1024)

    def respond(request):
        if "/api/models/" in request.url.path:
            return httpx.Response(200, stream=Chunks([json.dumps(metadata).encode()]))
        return httpx.Response(200, stream=Endless())

    client = httpx.Client(transport=httpx.MockTransport(respond), trust_env=False)
    with pytest.raises(PreparationError, match="model_source_metadata_mismatch"):
        transfer(selected, tmp_path / "new", None, Budget(), client=client)
    assert consumed == [0, 1]
    assert list((tmp_path / "new").iterdir()) == []
