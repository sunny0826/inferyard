"""Preparation-only engine acquisition; fixtures never send network requests."""

import hashlib
import json
import subprocess
import sys

import pytest

from inferyard.cli import main

SOURCE = (
    "https://github.com/PrismML-Eng/llama.cpp/releases/download/"
    "prism-b10743-adfffbe/llama-prism-b10743-adfffbe-bin-macos-arm64.tar.gz"
)


@pytest.mark.parametrize(
    "source",
    [
        SOURCE.replace("https:", "http:"),
        SOURCE.replace("github.com", "github.com:443"),
        SOURCE + "?token=SECRET",
        SOURCE + "#SECRET",
        SOURCE.replace("github.com", "SECRET@github.com"),
        SOURCE.replace("releases", "发布"),
        SOURCE.replace("releases", "%E5%8F%91%E5%B8%83"),
        SOURCE.replace("releases", ".."),
        SOURCE.replace("releases", "%2e%2e"),
        SOURCE.replace("releases", "%2f"),
        SOURCE.replace("/download/", "/download//"),
        SOURCE.replace("/download/prism-b10743", "/download/ prism-b10743"),
        "https://github.com",
        SOURCE.replace("github.com", "not_a_host"),
        SOURCE.replace("github.com", "github.com."),
        f"SOURCE with space {SOURCE}",
    ],
)
def test_invalid_source_uses_fixed_reason_without_echo(source, tmp_path, capsys):
    out = tmp_path / "new"
    assert main(["engine", "acquire", "--source", source, "--out", str(out)]) == 2
    captured = capsys.readouterr()
    assert json.loads(captured.out)["limitations"] == ["invalid_engine_source"]
    assert "SECRET" not in captured.out + captured.err
    assert not out.exists()


@pytest.mark.parametrize("seconds", [0, -1, "nan", "inf"])
def test_timeout_must_be_a_positive_finite_number(seconds, tmp_path, capsys):
    assert (
        main(
            [
                "engine",
                "acquire",
                "--source",
                SOURCE,
                "--out",
                str(tmp_path / "new"),
                "--timeout-seconds",
                str(seconds),
            ]
        )
        == 2
    )
    assert json.loads(capsys.readouterr().out)["limitations"] == ["invalid_engine_source"]


def test_acquire_success_publishes_files_and_status(tmp_path, monkeypatch, capsys):
    from tests.unit.test_engine_source_download import FILENAME, PAYLOAD, body_server, loopback

    with body_server("redirect") as (server, requests):
        loopback(monkeypatch, server.server_port)
        code = main(["engine", "acquire", "--source", SOURCE, "--out", str(tmp_path / "new")])
    assert code == 0
    payload = json.loads(capsys.readouterr().out)
    assert payload["command"] == "engine acquire"
    assert payload["status"] == "prepared"
    details = payload["details"]
    out = tmp_path / "new"
    assert (out / FILENAME).read_bytes() == PAYLOAD
    assert details["sha256"] == hashlib.sha256(PAYLOAD).hexdigest()
    assert details["bytes"] == len(PAYLOAD)
    assert details["ready_to_run"] is False
    record = json.loads((out / "engine-source.json").read_text())
    assert record["definition"] == "remote_engine_source.v1"
    assert record["sha256"] == details["sha256"]
    assert [path for path, *_ in requests] == [f"/{FILENAME}", f"/cdn/{FILENAME}"]


@pytest.mark.parametrize(
    "arguments", [["--help"], ["engine", "acquire", "--help"], ["run", "--help"]]
)
def test_help_keeps_engine_backend_lazy_and_run_local(arguments):
    script = """
import importlib.abc
import json
import sys
class RejectBackend(importlib.abc.MetaPathFinder):
    def find_spec(self, fullname, path=None, target=None):
        if fullname.startswith(('inferyard.platforms.engine_source', 'httpx')):
            raise AssertionError(fullname)
sys.meta_path.insert(0, RejectBackend())
from inferyard.cli import main
raise SystemExit(main(json.loads(sys.argv[1])))
"""
    result = subprocess.run(
        [sys.executable, "-c", script, json.dumps(arguments)],
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0, result.stderr
    if arguments[0] == "run":
        assert "--source" not in result.stdout


def test_run_rejects_engine_url_option(capsys):
    assert main(["run", "--source", SOURCE]) == 2
    assert json.loads(capsys.readouterr().out)["limitations"] == ["invalid_input"]
