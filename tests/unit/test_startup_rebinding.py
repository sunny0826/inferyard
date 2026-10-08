"""Endpoint rebinding against sealed synthetic evidence; no service requests."""

from dataclasses import replace

import pytest

from inferyard.application.types import CommandRequest
from inferyard.config.loader import load_config
from inferyard.contracts.validation import Document
from inferyard.platforms.identity import PreflightError
from inferyard.runtime import batch_state, runner
from tests import helpers


@pytest.mark.parametrize("inline", [False, True])
@pytest.mark.parametrize(
    "endpoint,host,port",
    [
        ("http://127.0.0.2:9090", "127.0.0.2", "9090"),
        ("https://localhost", "localhost", "443"),
    ],
)
def test_sealed_rerun_and_batch_rebind_endpoint(
    config_path, tmp_path, monkeypatch, inline, endpoint, host, port
):
    loaded = load_config(config_path)
    config = loaded.config.to_dict()
    fixed = ["--model", config["model"]["local_path"], "-t", "6"]
    old = (
        ["--host=127.0.0.1", "--port=8080"] if inline else ["--host", "127.0.0.1", "--port", "8080"]
    )
    new = [f"--host={host}", f"--port={port}"] if inline else ["--host", host, "--port", port]
    config["engine"]["startup_args"] = fixed + old
    loaded = replace(loaded, config=Document.parse("config", config))
    monkeypatch.setattr(helpers, "load_config", lambda path: loaded)
    root = helpers.fixture_run(tmp_path / "source", states=["completed"])
    before = {p.name: p.read_bytes() for p in root.iterdir() if p.is_file()}

    def ticks(pid):
        if pid == config["endpoint"]["server_pid"]:
            raise PreflightError("service_process_unavailable")
        return 98765

    monkeypatch.setattr(runner, "process_start_ticks", ticks)
    monkeypatch.setattr(batch_state, "process_start_ticks", ticks)
    request = CommandRequest("run", from_run=root, server_pid=45678, endpoint_url=endpoint)
    rebound, _ = runner.load_rerun(request)
    batched, _ = batch_state.bind_service(loaded, endpoint, 45678)
    for value in (rebound, batched):
        actual = value.config.to_dict()
        assert actual["engine"]["startup_args"] == fixed + new
        assert actual["endpoint"]["url"] == endpoint
        assert actual["endpoint"]["server_pid"] == 45678
        assert actual["endpoint"]["process_start_ticks"] == 98765
    assert before == {p.name: p.read_bytes() for p in root.iterdir() if p.is_file()}


@pytest.mark.parametrize("inline", [False, True])
def test_batch_retains_existing_short_port_binding(config_path, monkeypatch, inline):
    loaded = load_config(config_path)
    config = loaded.config.to_dict()
    config["engine"]["startup_args"] = ["-p=8080"] if inline else ["-p", "8080"]
    loaded = replace(loaded, config=Document.parse("config", config))
    monkeypatch.setattr(batch_state, "process_start_ticks", lambda pid: 98765)
    rebound, _ = batch_state.bind_service(loaded, "http://127.0.0.1:9090", 45678)
    assert rebound.config.to_dict()["engine"]["startup_args"] == (
        ["-p=9090"] if inline else ["-p", "9090"]
    )
