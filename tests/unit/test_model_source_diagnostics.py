"""Acquisition diagnostics are enumerated classifications, never exception text or names."""

import json
import ssl

import httpx
import pytest

from inferyard.cli import main
from inferyard.config.preparation_io import PreparationError
from inferyard.platforms.model_source_download import transfer
from inferyard.platforms.model_source_host import Budget
from tests.unit.test_model_acquire_cli import SOURCE
from tests.unit.test_model_source import source
from tests.unit.test_model_source_download import fixture_client
from tests.unit.test_model_source_watchdog import scripted_child


@pytest.mark.parametrize(
    "stage,exception,expected",
    [
        ("metadata", httpx.ConnectTimeout("SECRET URL header token"), "connect_timeout"),
        ("download-open", httpx.ConnectError("SECRET URL header token"), "connect_error"),
        ("metadata", httpx.ReadTimeout("SECRET"), "read_timeout"),
        ("metadata", httpx.RemoteProtocolError("SECRET"), "protocol_error"),
        ("metadata", type("SECRETException", (Exception,), {})("SECRET"), "internal_error"),
        ("metadata", OSError("SECRET"), "io_error"),
    ],
)
def test_failure_stage_and_exception_are_allowlisted(tmp_path, stage, exception, expected):
    client, _ = fixture_client()
    original = client.send

    def send(request, **kwargs):
        if stage == "metadata" or "/resolve/" in request.url.path:
            raise exception
        return original(request, **kwargs)

    client.send = send
    with pytest.raises(PreparationError) as caught:
        transfer(source("huggingface"), tmp_path / "new", None, Budget(), client=client)
    detail = caught.value.diagnostics
    assert detail["stage"] == stage
    assert detail["exception_class"] == expected
    assert "SECRET" not in str(caught.value) + json.dumps(detail)


def test_tls_classification_uses_exception_type_not_message(tmp_path):
    error = httpx.ConnectError("SECRET response")
    error.__cause__ = ssl.SSLError("SECRET certificate")
    client, _ = fixture_client()

    def send(*args, **kwargs):
        raise error

    client.send = send
    with pytest.raises(PreparationError) as caught:
        transfer(source("huggingface"), tmp_path / "new", None, Budget(), client=client)
    assert caught.value.diagnostics["exception_class"] == "tls_error"


def test_http_rejection_and_publish_failure_keep_stage(tmp_path, monkeypatch):
    client, _ = fixture_client(status=403)
    with pytest.raises(PreparationError) as caught:
        transfer(source("huggingface"), tmp_path / "http", None, Budget(), client=client)
    assert caught.value.diagnostics["stage"] == "download-open"
    assert caught.value.diagnostics["exception_class"] == "http_status"

    from inferyard.platforms import model_source_download as download

    def broken(*args):
        raise OSError("SECRET local file")

    monkeypatch.setattr(download, "publish", broken)
    client, _ = fixture_client()
    with pytest.raises(PreparationError) as caught:
        transfer(source("huggingface"), tmp_path / "disk", None, Budget(), client=client)
    assert caught.value.diagnostics["stage"] == "publish"
    assert caught.value.diagnostics["exception_class"] == "io_error"
    assert caught.value.diagnostics["bytes_written"] > 0
    assert caught.value.code == 4


def test_real_worker_to_cli_never_exposes_dynamic_exception_name(tmp_path, monkeypatch, capsys):
    children = scripted_child(
        monkeypatch,
        """
from inferyard.platforms import model_source_worker as worker
def fail(*args, **kwargs):
    raise type('SECRETException', (Exception,), {})('https://SECRET/?token=SECRET')
worker.transfer = fail
worker.main()
""",
    )
    assert main(["model", "acquire", "--source", SOURCE, "--out", str(tmp_path / "new")]) == 2
    captured = capsys.readouterr()
    result = json.loads(captured.out)
    assert result["limitations"] == ["model_acquire_incomplete"]
    assert result["details"]["diagnostics"]["exception_class"] == "internal_error"
    diagnostic_line = captured.err.split("acquisition diagnostics: ", 1)[1]
    assert json.loads(diagnostic_line)["stage"] == "preflight"
    assert "SECRET" not in captured.out + captured.err
    assert children[0].poll() is not None
