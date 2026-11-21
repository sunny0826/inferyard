"""Sanitized failure diagnostics for model and engine acquisition."""

import errno
import json
from types import SimpleNamespace

import httpx
import pytest

from inferyard.application import engine_source as app_engine_source
from inferyard.application import model_source as app_model_source
from inferyard.cli import main
from inferyard.provisioning import model_source_download
from inferyard.provisioning.acquire_errors import (
    classify_http_status,
    classify_httpx_error,
    classify_os_error,
    failure_details,
)
from inferyard.provisioning.preparation_io import PreparationError

REVISION = "499bc8821c6b12b4e53c5bffcb21ec206f212d81"
SOURCE = (
    f"https://huggingface.co/ggml-org/models-moved/blob/{REVISION}/tinyllamas/stories260K.gguf"
)
SECRET = "SUPER_SECRET_TOKEN_VALUE_12345"
BODY_SECRET = "UPSTREAM_RAW_BODY_SECRET"


@pytest.mark.parametrize(
    ("status_code", "expected"),
    [
        (401, "auth_required"),
        (403, "auth_required"),
        (404, "not_found"),
        (429, "rate_limited"),
        (500, "upstream_http_error"),
        (502, "upstream_http_error"),
        (302, "upstream_http_error"),
    ],
)
def test_classify_http_status(status_code, expected):
    assert classify_http_status(status_code) == expected


@pytest.mark.parametrize(
    ("exc", "expected"),
    [
        (httpx.ReadTimeout("https://secret.example/timeout"), "network_timeout"),
        (httpx.ConnectTimeout("connect timeout"), "network_timeout"),
        (httpx.ConnectError("https://secret.example/connect"), "connection_error"),
        (httpx.NetworkError("network reset"), "connection_error"),
        (httpx.RemoteProtocolError("bad protocol"), "connection_error"),
        (httpx.DecodingError("decode failed"), "transfer_error"),
        (httpx.HTTPError("generic http"), "transfer_error"),
    ],
)
def test_classify_httpx_error(exc, expected):
    assert classify_httpx_error(exc) == expected


@pytest.mark.parametrize(
    ("exc", "expected"),
    [
        (OSError(errno.ENOSPC, "no space"), "disk_full"),
        (OSError(errno.EACCES, "denied"), "permission_denied"),
        (OSError(errno.EPERM, "not permitted"), "permission_denied"),
        (PermissionError("denied"), "permission_denied"),
        (OSError(errno.EIO, "io failure"), "io_error"),
    ],
)
def test_classify_os_error(exc, expected):
    assert classify_os_error(exc) == expected


def test_preparation_error_details_backward_compatible():
    plain = PreparationError("invalid_input")
    assert plain.reason == "invalid_input"
    assert plain.code == 2
    assert plain.details is None

    with_code = PreparationError("io_error", 4)
    assert with_code.code == 4
    assert with_code.details is None

    details = failure_details("download_open", "not_found")
    detailed = PreparationError("model_acquire_incomplete", 2, details=details)
    assert detailed.details == {"failure_stage": "download_open", "failure_cause": "not_found"}


def test_model_acquire_missing_and_invalid_token_env(monkeypatch, tmp_path):
    monkeypatch.delenv("TEST_MISSING_TOKEN", raising=False)
    with pytest.raises(PreparationError) as missing_exc:
        model_source_download.acquire(SOURCE, tmp_path / "missing", token_env="TEST_MISSING_TOKEN")
    assert missing_exc.value.reason == "model_acquire_incomplete"
    assert missing_exc.value.details == {
        "failure_stage": "token",
        "failure_cause": "token_env_missing",
    }

    monkeypatch.setenv("TEST_BAD_TOKEN", f"{SECRET}\n")
    with pytest.raises(PreparationError) as bad_exc:
        model_source_download.acquire(SOURCE, tmp_path / "bad", token_env="TEST_BAD_TOKEN")
    assert bad_exc.value.reason == "model_acquire_incomplete"
    assert bad_exc.value.details == {
        "failure_stage": "token",
        "failure_cause": "token_env_missing",
    }
    assert SECRET not in str(bad_exc.value.details)
    assert "TEST_BAD_TOKEN" not in str(bad_exc.value.details)


@pytest.mark.parametrize(
    ("status_code", "expected_cause"),
    [
        (401, "auth_required"),
        (403, "auth_required"),
        (404, "not_found"),
        (429, "rate_limited"),
        (503, "upstream_http_error"),
    ],
)
def test_model_acquire_http_status_diagnostics(status_code, expected_cause, tmp_path):
    def handler(request):
        return httpx.Response(
            status_code,
            headers={"X-Secret": SECRET},
            content=BODY_SECRET.encode(),
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))
    with client, pytest.raises(PreparationError) as exc_info:
        model_source_download.acquire(SOURCE, tmp_path / "out", client=client)

    assert exc_info.value.reason == "model_acquire_incomplete"
    assert exc_info.value.details == {
        "failure_stage": "download_open",
        "failure_cause": expected_cause,
    }
    serialized = json.dumps(exc_info.value.details)
    assert SECRET not in serialized
    assert BODY_SECRET not in serialized
    assert "huggingface.co" not in serialized


def test_model_acquire_unexpected_content_encoding(tmp_path):
    def handler(request):
        return httpx.Response(
            200,
            headers={"content-encoding": "gzip"},
            stream=httpx.ByteStream(b"compressed"),
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))
    with client, pytest.raises(PreparationError) as exc_info:
        model_source_download.acquire(SOURCE, tmp_path / "out", client=client)

    assert exc_info.value.reason == "model_acquire_incomplete"
    assert exc_info.value.details == {
        "failure_stage": "download_open",
        "failure_cause": "unexpected_content_encoding",
    }


@pytest.mark.parametrize(
    ("raised", "expected_cause"),
    [
        (httpx.ReadTimeout("https://huggingface.co/secret-timeout"), "network_timeout"),
        (httpx.ConnectError("https://huggingface.co/secret-connect"), "connection_error"),
        (httpx.RemoteProtocolError("bad chunk"), "connection_error"),
        (httpx.DecodingError("bad encoding"), "transfer_error"),
    ],
)
def test_model_acquire_httpx_error_diagnostics(raised, expected_cause, tmp_path):
    def handler(request):
        raise raised

    client = httpx.Client(transport=httpx.MockTransport(handler))
    with client, pytest.raises(PreparationError) as exc_info:
        model_source_download.acquire(SOURCE, tmp_path / "out", client=client)

    assert exc_info.value.reason == "model_acquire_incomplete"
    assert exc_info.value.details == {
        "failure_stage": "download_body",
        "failure_cause": expected_cause,
    }
    assert "huggingface.co" not in json.dumps(exc_info.value.details)


def test_model_acquire_mid_stream_timeout_cleans_part_and_classifies(tmp_path):
    class FailingStream(httpx.SyncByteStream):
        def __iter__(self):
            yield b"partial-"
            raise httpx.ReadTimeout("https://huggingface.co/secret-mid-stream")

    def handler(request):
        return httpx.Response(
            200,
            headers={"content-encoding": "identity"},
            stream=FailingStream(),
        )

    out = tmp_path / "out"
    client = httpx.Client(transport=httpx.MockTransport(handler))
    with client, pytest.raises(PreparationError) as exc_info:
        model_source_download.acquire(SOURCE, out, client=client)

    assert exc_info.value.reason == "model_acquire_incomplete"
    assert exc_info.value.details == {
        "failure_stage": "download_body",
        "failure_cause": "network_timeout",
    }
    assert not (out / "stories260K.gguf.part").exists()
    assert not (out / "stories260K.gguf").exists()


def test_model_acquire_success_returns_expected_record(tmp_path):
    def handler(request):
        return httpx.Response(
            200,
            headers={"content-encoding": "identity"},
            stream=httpx.ByteStream(b"gguf-ok"),
        )

    out = tmp_path / "ok"
    client = httpx.Client(transport=httpx.MockTransport(handler))
    with client:
        result = model_source_download.acquire(SOURCE, out, client=client)

    assert result["bytes"] == 7
    assert (out / "stories260K.gguf").read_bytes() == b"gguf-ok"


@pytest.mark.parametrize(
    ("os_err", "expected_cause"),
    [
        (OSError(errno.ENOSPC, "/secret/disk/full"), "disk_full"),
        (OSError(errno.EACCES, "/secret/permission/denied"), "permission_denied"),
        (OSError(errno.EIO, "/secret/io/err"), "io_error"),
    ],
)
def test_model_acquire_os_error_diagnostics(monkeypatch, os_err, expected_cause, tmp_path):
    def handler(request):
        return httpx.Response(
            200,
            headers={"content-encoding": "identity"},
            stream=httpx.ByteStream(b"gguf-bytes"),
        )

    def fail_publish(*_args, **_kwargs):
        raise os_err

    monkeypatch.setattr(model_source_download, "_publish", fail_publish)
    client = httpx.Client(transport=httpx.MockTransport(handler))
    with client, pytest.raises(PreparationError) as exc_info:
        model_source_download.acquire(SOURCE, tmp_path / "out", client=client)

    assert exc_info.value.reason == "io_error"
    assert exc_info.value.code == 4
    assert exc_info.value.details == {
        "failure_stage": "disk_write",
        "failure_cause": expected_cause,
    }
    assert "/secret/" not in json.dumps(exc_info.value.details)


def test_application_model_and_engine_source_forward_details_and_stderr(monkeypatch, capsys, tmp_path):
    model_req = SimpleNamespace(
        command="model acquire",
        model_source_url=SOURCE,
        out=tmp_path / "m",
        model_token_env=None,
        model_timeout_seconds=5.0,
    )
    engine_req = SimpleNamespace(
        command="engine acquire",
        engine_source_url="https://example.invalid/engine.tar.gz",
        out=tmp_path / "e",
        engine_timeout_seconds=5.0,
    )

    def fail_model(*_args, **_kwargs):
        raise PreparationError(
            "model_acquire_incomplete",
            details=failure_details("download_open", "rate_limited"),
        )

    def fail_engine(*_args, **_kwargs):
        raise PreparationError(
            "engine_acquire_incomplete",
            details=failure_details("download_body", "network_timeout"),
        )

    monkeypatch.setattr(model_source_download, "acquire", fail_model)
    code, result = app_model_source.execute(model_req)
    captured = capsys.readouterr()
    assert code == 2
    assert result.status == "blocked"
    assert result.limitations == ("model_acquire_incomplete",)
    assert result.details == {"failure_stage": "download_open", "failure_cause": "rate_limited"}
    assert captured.err.strip() == "model acquisition failed: model_acquire_incomplete (rate_limited)"

    import inferyard.provisioning.engine_source_download as engine_dl

    monkeypatch.setattr(engine_dl, "acquire", fail_engine)
    code, result = app_engine_source.execute(engine_req)
    captured = capsys.readouterr()
    assert code == 2
    assert result.status == "blocked"
    assert result.limitations == ("engine_acquire_incomplete",)
    assert result.details == {"failure_stage": "download_body", "failure_cause": "network_timeout"}
    assert captured.err.strip() == (
        "engine acquisition failed: engine_acquire_incomplete (network_timeout)"
    )


def test_application_without_details_preserves_none_and_stderr_format(monkeypatch, capsys, tmp_path):
    engine_req = SimpleNamespace(
        command="engine acquire",
        engine_source_url="https://example.invalid/engine.tar.gz",
        out=tmp_path / "e",
        engine_timeout_seconds=5.0,
    )
    import inferyard.provisioning.engine_source_download as engine_dl

    monkeypatch.setattr(
        engine_dl,
        "acquire",
        lambda *_a, **_kw: (_ for _ in ()).throw(PreparationError("invalid_engine_source")),
    )
    code, result = app_engine_source.execute(engine_req)
    captured = capsys.readouterr()
    assert code == 2
    assert result.details is None
    assert captured.err.strip() == "engine acquisition failed: invalid_engine_source"


def test_cli_model_acquire_sanitized_failure_without_secret_echo(monkeypatch, tmp_path, capsys):
    monkeypatch.setenv("MY_HF_SECRET", f"{SECRET}\n")
    out = tmp_path / "new"
    code = main(
        [
            "model",
            "acquire",
            "--source",
            SOURCE,
            "--out",
            str(out),
            "--token-env",
            "MY_HF_SECRET",
        ]
    )
    captured = capsys.readouterr()
    assert code == 2
    payload = json.loads(captured.out)
    assert payload["limitations"] == ["model_acquire_incomplete"]
    assert payload["details"] == {
        "failure_stage": "token",
        "failure_cause": "token_env_missing",
    }
    assert "model acquisition failed: model_acquire_incomplete (token_env_missing)" in captured.err
    assert SECRET not in captured.out + captured.err
    assert "MY_HF_SECRET" not in captured.out + captured.err
