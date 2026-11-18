"""The actual CLI preserves JSON, errors, persistence and cold metadata starts."""

import json
import os
import socket
import subprocess
import sys

import pytest

from inferyard import cli
from inferyard.application import memory_check
from inferyard.evidence.memory_check import read_memory_check
from tests.unit.test_memory_check import app, observed, report


@pytest.fixture
def macos(monkeypatch):
    monkeypatch.setattr(memory_check.platform, "system", lambda: "Darwin")


def test_output_does_not_leak_internal_paths_or_sentinels_or_signal_user_processes(
    macos, monkeypatch, tmp_path, capsys
):
    from inferyard.platforms import memory_macos
    from inferyard.runtime import host_files
    from tests.unit.test_memory_check import snapshot

    sentinel = "MEMORY_DIAGNOSTIC_PRIVATE_SENTINEL"
    row = observed(10, bundle=app(key=f"/private/{sentinel}"))
    row["executable"] = f"/private/{sentinel}/tool"
    row["environment"] = {"SENTINEL": sentinel}
    monkeypatch.setenv("MEMORY_DIAGNOSTIC_SENTINEL", sentinel)
    monkeypatch.setattr(memory_macos, "collect", lambda **_: snapshot([row]))
    monkeypatch.setattr(memory_check.os, "getuid", lambda: 501, raising=False)

    def forbidden(*args, **kwargs):
        raise AssertionError("read-only command performed external operation")

    monkeypatch.setattr(socket.socket, "connect", forbidden)
    monkeypatch.setattr(os, "kill", forbidden)
    monkeypatch.setattr(host_files, "publish_state", forbidden)
    out = tmp_path / "private-check"
    assert cli.main(["memory-check", "--out", str(out)]) == 0
    output = capsys.readouterr()
    persisted = (out / "memory-check.json").read_text()
    assert sentinel not in output.out + output.err + persisted
    assert "/private/" not in json.dumps(json.loads(output.out)["details"]) + persisted
