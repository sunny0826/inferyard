"""End-to-end `model acquire`: the real CLI chain against a real loopback HTTP server."""

import hashlib
import json

from inferyard.cli import main
from inferyard.platforms.model_source import ModelSource
from tests.unit.test_model_acquire_cli import SOURCE
from tests.unit.test_model_source_download import FILENAME, PAYLOAD, body_server


def loopback(monkeypatch, port):
    monkeypatch.setattr(
        ModelSource,
        "download_url",
        property(lambda self: f"http://127.0.0.1:{port}/{FILENAME}"),
    )


def test_acquire_redirect_then_publish(tmp_path, monkeypatch, capsys):
    with body_server("redirect") as (server, requests):
        loopback(monkeypatch, server.server_port)
        code = main(["model", "acquire", "--source", SOURCE, "--out", str(tmp_path / "new")])
    assert code == 0
    payload = json.loads(capsys.readouterr().out)
    assert payload["command"] == "model acquire"
    assert payload["status"] == "prepared"
    details = payload["details"]
    out = tmp_path / "new"
    assert (out / FILENAME).read_bytes() == PAYLOAD
    assert details["sha256"] == hashlib.sha256(PAYLOAD).hexdigest()
    assert details["ready_to_run"] is False
    record = json.loads((out / "source.json").read_text())
    assert record["definition"] == "remote_model_source.v3"
    assert record["sha256"] == details["sha256"]
    assert record["bytes"] == len(PAYLOAD)
    assert [path for path, *_ in requests] == [f"/{FILENAME}", f"/cdn/{FILENAME}"]


def test_acquire_http_failure_maps_to_blocked(tmp_path, monkeypatch, capsys):
    with body_server("missing") as (server, _):
        loopback(monkeypatch, server.server_port)
        code = main(["model", "acquire", "--source", SOURCE, "--out", str(tmp_path / "new")])
    assert code == 2
    payload = json.loads(capsys.readouterr().out)
    assert payload["status"] == "blocked"
    assert payload["limitations"] == ["model_acquire_incomplete"]
    assert not (tmp_path / "new" / FILENAME).exists()
    assert not (tmp_path / "new" / "source.json").exists()


def test_acquire_existing_output_never_sends_a_request(tmp_path, monkeypatch, capsys):
    (tmp_path / "new").mkdir()
    with body_server() as (server, requests):
        loopback(monkeypatch, server.server_port)
        code = main(["model", "acquire", "--source", SOURCE, "--out", str(tmp_path / "new")])
    assert code == 2
    assert json.loads(capsys.readouterr().out)["limitations"] == ["output_exists"]
    assert requests == []
