"""The actual CLI preserves JSON, errors, persistence and cold metadata starts."""

import json
import os
import socket
import subprocess
import sys

import pytest

from inferyard import cli
from inferyard.application import memory_check
from inferyard.contracts.memory_check import read_memory_check
from tests.unit.test_memory_check import app, observed, report


@pytest.fixture
def macos(monkeypatch):
    monkeypatch.setattr(memory_check.platform, "system", lambda: "Darwin")


def test_real_dispatch_persists_and_refuses_overwrite(macos, monkeypatch, tmp_path, capsys):
    data = report([observed(10, bundle=app())])
    monkeypatch.setattr(memory_check, "inspect_memory", lambda _: data)
    out = tmp_path / "new"
    assert cli.main(["memory-check", "--protect-pid", "10", "--out", str(out)]) == 0
    stdout = json.loads(capsys.readouterr().out)
    assert stdout["details"] == read_memory_check(out / "memory-check.json") == data
    original = (out / "memory-check.json").read_bytes()
    assert cli.main(["memory-check", "--out", str(out)]) == 4
    assert json.loads(capsys.readouterr().out)["status"] == "error"
    assert (out / "memory-check.json").read_bytes() == original


def test_partial_json_reaches_user_with_exit_three(macos, monkeypatch, capsys):
    data = report([observed(10, missing="access_denied")])
    monkeypatch.setattr(memory_check, "inspect_memory", lambda _: data)
    assert cli.main(["memory-check"]) == 3
    value = json.loads(capsys.readouterr().out)
    assert value["status"] == "partial"
    assert value["details"]["processes"][0]["rss"]["value"] is None


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
    monkeypatch.setattr(memory_check.os, "getuid", lambda: 501)

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


@pytest.mark.parametrize("pid", ["0", "-1", "not-a-pid"])
def test_invalid_protection_rejects_before_collecting(pid, monkeypatch, capsys):
    def reject(_):
        raise AssertionError("collector must not run")

    monkeypatch.setattr(memory_check, "inspect_memory", reject)
    assert cli.main(["memory-check", "--protect-pid", pid]) == 2
    assert json.loads(capsys.readouterr().out)["limitations"] == ["invalid_input"]


def test_other_platform_is_explicitly_unsupported(monkeypatch, capsys):
    monkeypatch.setattr(memory_check.platform, "system", lambda: "Linux")
    assert cli.main(["memory-check"]) == 2
    value = json.loads(capsys.readouterr().out)
    assert value["limitations"] == ["unsupported_platform"]
    assert value["details"] is None


@pytest.mark.parametrize("error,code", [(KeyboardInterrupt(), 130), (OSError(), 4)])
def test_cancel_and_io_fault_preserve_json(macos, monkeypatch, capsys, error, code):
    def fail(_):
        raise error

    monkeypatch.setattr(memory_check, "inspect_memory", fail)
    assert cli.main(["memory-check"]) == code
    assert json.loads(capsys.readouterr().out)["details"] is None


@pytest.mark.parametrize(
    "arguments",
    [
        ["memory-check", "--help"],
        ["--schema", "memory_check"],
        ["--help"],
        ["--versions"],
    ],
)
def test_metadata_never_imports_collector(arguments):
    script = """
import importlib.abc, json, sys
class Reject(importlib.abc.MetaPathFinder):
    def find_spec(self, fullname, path=None, target=None):
        blocked = (
            "psutil", "inferyard.platforms.memory_macos", "inferyard.application.memory_check",
        )
        if fullname in blocked:
            raise AssertionError(fullname)
sys.meta_path.insert(0, Reject())
from inferyard.cli import main
raise SystemExit(main(json.loads(sys.argv[1])))
"""
    result = subprocess.run(
        [sys.executable, "-c", script, json.dumps(arguments)],
        capture_output=True,
        text=True,
        env={**os.environ, "NO_COLOR": "1"},
    )
    assert result.returncode == 0, result.stderr
