"""Official binding script output reaches real CLI config loading with mock engine transport."""

import json
import tomllib
from dataclasses import replace
from pathlib import Path

import pytest

import inferyard.runtime.runner as running
from inferyard.cli import main
from inferyard.config import service_binding
from inferyard.config.loader import load_config
from inferyard.config.toml_writer import render
from inferyard.contracts.validation import Document
from inferyard.errors import PreflightError
from inferyard.analysis.ledger import read_trial
from tests.integration.test_lab_cli import lab_scenario, write_config  # noqa: F401
from tests.integration.test_native_cli import native_scenario as _native_scenario
from tests.integration.test_runner import scenario  # noqa: F401
from tests.unit.bind_service_helpers import bind
from tests.unit.bind_service_helpers import binder as _binder

native_scenario = _native_scenario
binder = _binder


@pytest.fixture
def candidate(config_path, tmp_path, monkeypatch):
    config = load_config(config_path).config.to_dict()
    path = tmp_path / "candidate.toml"
    monkeypatch.setattr(service_binding, "require_platform", lambda *args: None)
    monkeypatch.setattr(service_binding, "process_start_ticks", lambda pid: 98765)
    return config, path


@pytest.mark.parametrize("inline", [False, True])
@pytest.mark.parametrize("endpoint", ["http://127.0.0.2:9090", "https://localhost"])
@pytest.mark.parametrize("mismatch", [False, True])
def test_bind_cli_replaces_only_endpoint_arguments(
    candidate, tmp_path, monkeypatch, capsys, inline, endpoint, mismatch
):
    config, path = candidate
    old = (
        ["--host=127.0.0.1", "--port=8080"] if inline else ["--host", "127.0.0.1", "--port", "8080"]
    )
    host, port = ("localhost", "443") if endpoint.startswith("https") else ("127.0.0.2", "9090")
    new = [f"--host={host}", f"--port={port}"] if inline else ["--host", host, "--port", port]
    unchanged = ["--model", config["model"]["local_path"], "-t", "6"]
    config["engine"]["startup_args"] = unchanged + old
    path.write_text(render(config))
    before = path.read_bytes()
    command = [config["engine"]["binary_path"], *unchanged, *new]
    if mismatch:
        command[4] = "7"
    monkeypatch.setattr(service_binding, "arguments", lambda pid: command)
    out = tmp_path / "bound"
    code = main(
        [
            "config",
            "bind",
            "--candidate",
            str(path),
            "--pid",
            "123",
            "--endpoint",
            endpoint,
            "--out",
            str(out),
        ]
    )
    result = json.loads(capsys.readouterr().out)
    assert path.read_bytes() == before
    if mismatch:
        assert code == 2 and result["limitations"] == ["startup_arguments_mismatch"]
        assert not out.exists()
    else:
        assert code == 0 and result["status"] == "prepared"
        bound = load_config(out / "config.toml").config.to_dict()
        assert bound["engine"]["startup_args"] == unchanged + new
        assert bound["endpoint"]["url"] == endpoint
        assert bound["endpoint"]["server_pid"] == 123
        assert bound["endpoint"]["process_start_ticks"] == 98765
        assert result["details"]["model_requests_sent"] == 0
