"""An operator cannot launch below its budget and always closes its owned process."""

import hashlib
import importlib.util
import json
import os
from copy import deepcopy
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.platforms.identity import PreflightError


@pytest.fixture
def operator(tmp_path, monkeypatch):
    scripts = Path(__file__).parents[2] / "scripts"
    monkeypatch.syspath_prepend(str(scripts))
    spec = importlib.util.spec_from_file_location(
        "windows_operator", scripts / "run_windows_mvp.py"
    )
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    monkeypatch.setattr(module, "ROOT", tmp_path)
    captured = []

    def collect(out, *, config_path):
        captured.append((out, config_path))
        return {"machine_json": str(out / "machine.json"), "reference_only": True}

    monkeypatch.setattr(module, "collect_machine", collect)
    module.captured_preparations = captured
    config = {
        "model": {},
        "engine": {"startup_args": []},
        "endpoint": {"url": "http://127.0.0.1:48857"},
        "output": {"min_available_memory_bytes": 8 * 1024**3},
    }
    for section, field, digest_field in (
        ("model", "local_path", "sha256"),
        ("model", "template_path", "template_sha256"),
        ("engine", "binary_path", "binary_sha256"),
    ):
        path = tmp_path / (field + ".fixture")
        path.write_bytes(field.encode())
        config[section][field] = str(path)
        config[section][digest_field] = hashlib.sha256(path.read_bytes()).hexdigest()
    document = SimpleNamespace(to_dict=lambda: deepcopy(config))
    monkeypatch.setattr(
        module, "load_config", lambda path: SimpleNamespace(config=document, source=path)
    )
    return module, config


def test_startup_reserve_keeps_loaded_floor_and_counts_model_bytes(operator):
    module, config = operator
    required = 9 * 1024**3 + len(b"local_path")
    assert module.prelaunch_budget(config, required)["accepted"]
    assert not module.prelaunch_budget(config, required - 1)["accepted"]


@pytest.mark.skipif(os.name != "nt", reason="Native Windows operator dispatch")
def test_insufficient_memory_never_starts_a_service(operator, tmp_path, monkeypatch):
    module, _ = operator
    monkeypatch.setattr(module, "memory_available", lambda: 6 * 1024**3)

    def forbidden(*args, **kwargs):
        raise AssertionError("No service may be started below the frozen budget")

    monkeypatch.setattr(module.subprocess, "Popen", forbidden)
    code, record = module.operate(tmp_path / "candidate.toml", tmp_path / "output", formal=True)
    assert code == 2 and record["model_requests_sent"] == 0
    assert not record["service_started"] and not record["benchmark_cli_started"]
    assert len(module.captured_preparations) == 1
    assert record["machine_preparation"]["reference_only"]
    assert json.loads((tmp_path / "output/operator.json").read_text())["status"] == "blocked"


@pytest.mark.skipif(os.name != "nt", reason="Native Windows operator dispatch")
def test_identity_failure_after_start_still_closes_the_owned_process(
    operator, tmp_path, monkeypatch
):
    module, _ = operator
    monkeypatch.setattr(module, "memory_available", lambda: 16 * 1024**3)

    class Owned:
        pid = 123
        returncode = None
        closed = False

        def poll(self):
            return self.returncode

        def terminate(self):
            self.closed = True
            self.returncode = 1

        def wait(self, timeout):
            return self.returncode

    owned = Owned()
    monkeypatch.setattr(module.subprocess, "Popen", lambda *args, **kwargs: owned)

    def unreadable(pid):
        raise PreflightError("service_identity_unreadable")

    monkeypatch.setattr(module, "process_start_ticks", unreadable)
    code, record = module.operate(tmp_path / "candidate.toml", tmp_path / "output")
    assert code == 2 and owned.closed and record["owned_server_closed"]
    assert not record["benchmark_cli_started"]
