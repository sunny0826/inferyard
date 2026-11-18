"""Windows public CLI dispatch, persisted contract and side-effect boundaries."""

import json
import os
import socket
import sys

import pytest

from inferyard import cli
from inferyard.application import memory_check
from inferyard.evidence.memory_check import read_memory_check
from inferyard.platforms import memory_windows
from tests.unit.memory_check_windows_helpers import observed, snapshot


@pytest.fixture
def windows(monkeypatch):
    monkeypatch.setattr(memory_check.platform, "system", lambda: "Windows")


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
