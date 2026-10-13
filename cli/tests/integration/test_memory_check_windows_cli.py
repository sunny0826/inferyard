"""Windows public CLI dispatch, persisted contract and side-effect boundaries."""

import json
import os
import socket
import sys

import pytest

from inferyard import cli
from inferyard.application import memory_check
from inferyard.contracts.memory_check import read_memory_check
from inferyard.platforms import memory_windows
from tests.unit.test_memory_check_windows import observed, snapshot


@pytest.fixture
def windows(monkeypatch):
    monkeypatch.setattr(memory_check.platform, "system", lambda: "Windows")


def test_public_cli_success_persistence_privacy_and_no_external_actions(
    windows, monkeypatch, tmp_path, capsys
):
    from inferyard.runtime import host_files

    monkeypatch.setattr(memory_windows, "collect", lambda **_: snapshot([observed()]))

    def forbidden(*args, **kwargs):
        raise AssertionError("read-only command performed external operation")

    monkeypatch.setattr(socket.socket, "connect", forbidden)
    monkeypatch.setattr(os, "kill", forbidden)
    monkeypatch.setattr(host_files, "publish_state", forbidden)
    out = tmp_path / "new"
    assert cli.main(["memory-check", "--protect-pid", "10", "--out", str(out)]) == 0
    result = json.loads(capsys.readouterr().out)
    data = read_memory_check(out / "memory-check.json")
    assert result["details"] == data
    assert data["platform"] == "Windows"
    assert data["processes"][0]["decision"]["status"] == "protected"
    assert "private-owner" not in json.dumps(data)
    assert "private-path" not in json.dumps(data)
    original = (out / "memory-check.json").read_bytes()
    assert cli.main(["memory-check", "--out", str(out)]) == 4
    capsys.readouterr()
    assert (out / "memory-check.json").read_bytes() == original


@pytest.mark.parametrize("mode", ["owner", "host", "budget", "protection", "private"])
def test_partial_reaches_public_cli(windows, monkeypatch, capsys, mode):
    row = observed()
    data = snapshot([row])
    arguments = ["memory-check"]
    if mode == "owner":
        row.update(owner=None, session=None, ownership_missing_reason="access_denied")
    elif mode == "host":
        data["memory_available"] = memory_windows.reading(None, "fixture", "unavailable")
    elif mode == "budget":
        data.update(total_pids=2, truncation_reason="process_limit")
    elif mode == "protection":
        arguments += ["--protect-pid", "999"]
    else:
        row["private_commit"] = memory_windows.reading(None, "fixture", "unavailable")
    monkeypatch.setattr(memory_windows, "collect", lambda **_: data)
    assert cli.main(arguments) == 3
    value = json.loads(capsys.readouterr().out)
    assert value["details"]["coverage"]["complete"] is False
    assert value["details"]["processes"][0]["decision"]["status"] == "unknown"


@pytest.mark.skipif(sys.platform != "win32", reason="Windows native token API")
def test_native_token_session_and_filetime_are_read_only():
    from inferyard.platforms.memory_windows_api import owner_session
    from inferyard.platforms.windows_api import process_start

    before = process_start(os.getpid())
    owner, session = owner_session(os.getpid())
    assert owner and type(session) is int and session >= 0
    assert owner_session(os.getpid()) == (owner, session)
    assert process_start(os.getpid()) == before
