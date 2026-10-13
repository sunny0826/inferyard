"""Global macOS lsof views must prove unique TCP ownership without hiding errors."""

import subprocess
from types import SimpleNamespace

import pytest

from inferyard.platforms import engine_fit_macos_listener as listeners
from inferyard.platforms.identity import PreflightError


@pytest.fixture
def query(monkeypatch):
    result = SimpleNamespace(
        returncode=0,
        stderr="",
        stdout="p321\0\nf5\0tIPv4\0PTCP\0n127.0.0.1:8080\0TST=LISTEN\0TQR=0\0\n",
    )

    def run(command, **kwargs):
        assert "-p" not in command
        assert command[0] == "/usr/sbin/lsof"
        assert "-iTCP:8080" in command and "-sTCP:LISTEN" in command
        assert kwargs["timeout"] == 2 and kwargs.get("shell", False) is False
        return result

    monkeypatch.setattr(listeners.subprocess, "run", run)
    return result


def test_one_global_matching_listener_is_proven(query):
    listeners.unique_listener(321, "127.0.0.1", 8080)


@pytest.mark.parametrize("change", ["stderr", "status", "status_with_output"])
def test_uncertain_global_query_never_means_no_competing_listener(query, change):
    if change == "stderr":
        query.stderr = "private permission detail"
    else:
        query.returncode = 1 if change == "status_with_output" else 2
    with pytest.raises(PreflightError, match="listener_identity_unavailable") as error:
        listeners.unique_listener(321, "127.0.0.1", 8080)
    assert "private permission detail" not in str(error.value)


@pytest.mark.parametrize(
    "text",
    [
        "pbad\0",
        "p0\0",
        "f5\0tIPv4\0",
        "p321\0f\0",
        "p321\0f5\0tIPv4\0tIPv4\0",
        "p321\0n127.0.0.1:8080\0",
    ],
)
def test_malformed_global_output_refuses_uncertain_evidence(query, text):
    query.stdout = text
    with pytest.raises(PreflightError, match="listener_identity_unavailable"):
        listeners.unique_listener(321, "127.0.0.1", 8080)


@pytest.mark.parametrize("exception", [PermissionError(), subprocess.TimeoutExpired("lsof", 2)])
def test_global_query_failures_are_fixed_reason_not_raw_native_errors(
    query, monkeypatch, exception
):
    def fail(*args, **kwargs):
        raise exception

    monkeypatch.setattr(listeners.subprocess, "run", fail)
    with pytest.raises(PreflightError, match="listener_identity_unavailable"):
        listeners.unique_listener(321, "127.0.0.1", 8080)


def test_no_global_listener_is_changed_not_success(query):
    query.returncode = 1
    query.stdout = ""
    with pytest.raises(PreflightError, match="listener_ambiguous_or_changed"):
        listeners.unique_listener(321, "127.0.0.1", 8080)
