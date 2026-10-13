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
from inferyard.evidence.ledger import read_trial
from inferyard.platforms.identity import PreflightError
from tests.integration.test_lab_cli import lab_scenario, write_config  # noqa: F401
from tests.integration.test_native_cli import native_scenario as _native_scenario
from tests.integration.test_runner import scenario  # noqa: F401
from tests.unit.test_bind_service_config import bind
from tests.unit.test_bind_service_config import binder as _binder

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


@pytest.mark.parametrize("reader", ["process_start_ticks", "arguments"])
@pytest.mark.parametrize(
    "error_type,message,reason,code",
    [
        (PreflightError, "service_process_unavailable", "service_process_unavailable", 2),
        (PreflightError, "service_identity_unreadable", "service_identity_unreadable", 2),
        (PreflightError, "unknown_identity_failure", "preflight_blocked", 2),
        (
            PreflightError,
            "service_identity_unreadable: --api-key fixture-secret",
            "preflight_blocked",
            2,
        ),
        (
            RuntimeError,
            "service_identity_unreadable: --api-key fixture-secret",
            "internal_error",
            4,
        ),
        (OSError, "service_identity_unreadable: --api-key fixture-secret", "io_error", 4),
    ],
)
def test_bind_cli_preserves_only_fixed_process_reasons(
    candidate, tmp_path, monkeypatch, capsys, reader, error_type, message, reason, code
):
    config, path = candidate
    path.write_text(render(config))
    before = path.read_bytes()

    def fail(pid):
        raise error_type(message)

    monkeypatch.setattr(service_binding, reader, fail)
    out = tmp_path / "blocked"
    assert (
        main(
            [
                "config",
                "bind",
                "--candidate",
                str(path),
                "--pid",
                "123",
                "--endpoint",
                config["endpoint"]["url"],
                "--out",
                str(out),
            ]
        )
        == code
    )
    captured = capsys.readouterr()
    result = json.loads(captured.out)
    assert result["status"] == ("blocked" if code == 2 else "error")
    assert result["limitations"] == [reason]
    assert "fixture-secret" not in captured.out + captured.err
    assert "unknown_identity_failure" not in captured.out + captured.err
    assert not out.exists() and path.read_bytes() == before


def test_manifest_binding_script_to_cli_probe_round_trip(
    native_scenario, binder, tmp_path, monkeypatch, capsys
):
    command, deps, service = native_scenario
    module, observed = binder
    config = command.config.config.to_dict()
    config["engine"]["environment"] = {"CUDA_VISIBLE_DEVICES": "0", "CUDA_CACHE_DISABLE": "1"}
    config["engine"]["asset_manifest"].append(
        {"path": r"D:\lab\cuda.dll", "role": "library", "bytes": 123, "sha256": "c" * 64}
    )
    config["quantization_artifact_binding"] = {
        "root": r"D:\lab",
        "files": {"model": "artifact/model.gguf"},
    }
    command = replace(
        command, config=replace(command.config, config=Document.parse("config", config))
    )
    candidate = tmp_path / "candidate.toml"
    write_config(command, candidate)
    original_bytes = candidate.read_bytes()
    bound = tmp_path / "bound.toml"
    observed["ticks"] = config["endpoint"]["process_start_ticks"]
    original = bind(
        module, observed, candidate, bound, monkeypatch, pid=config["endpoint"]["server_pid"]
    )
    capsys.readouterr()
    parsed = tomllib.loads(bound.read_text(encoding="utf-8"))
    assert parsed == original
    Document.parse("config", parsed)
    assert load_config(bound).config.to_dict() == original
    assert parsed["engine"]["asset_manifest"] == config["engine"]["asset_manifest"]
    assert parsed["quantization_artifact_binding"] == config["quantization_artifact_binding"]
    assert candidate.read_bytes() == original_bytes
    monkeypatch.setattr(running, "Dependencies", lambda **kwargs: deps)
    assert main(["probe", "--config", str(bound)]) == 0
    root = Path(json.loads(capsys.readouterr().out)["evidence_dir"])
    assert (
        read_trial(root)["config"]["engine"]["asset_manifest"] == config["engine"]["asset_manifest"]
    )
    assert len(service.requests) == 2
