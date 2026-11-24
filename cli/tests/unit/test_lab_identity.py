"""Process evidence redacts observed CLI credentials."""

from pathlib import PureWindowsPath
from types import SimpleNamespace

import pytest

import inferyard.platforms.lab_identity as binding
from inferyard.config.loader import load_config
from tests.lab_service import lab_config


@pytest.fixture
def process_binding(config_path, monkeypatch):
    config = lab_config(load_config(config_path).config.to_dict(), "kvmem")
    config["engine"]["asset_manifest"].append(
        {"path": r"D:\lab\engine.dll", "role": "library", "bytes": 10, "sha256": "d" * 64}
    )
    model = SimpleNamespace(device=1, inode=2, unchanged=lambda: True)
    engine = SimpleNamespace(path=config["engine"]["binary_path"], sha256="e" * 64)
    values = {
        "args": [engine.path, *config["engine"]["startup_args"]],
        "env": {},
        "maps": [SimpleNamespace(path=r"D:\lab\engine.dll")],
        "inode": 2,
    }
    process = SimpleNamespace(
        cmdline=lambda: values["args"],
        environ=lambda: values["env"],
        memory_maps=lambda **kwargs: values["maps"],
    )
    api = SimpleNamespace(path_stamp=lambda path: (1, values["inode"].to_bytes(16, "big")))

    def verify(expected, **kwargs):
        return {**expected, "listener_identity": "fixture", "details": kwargs["inspect_details"]()}

    monkeypatch.setattr(binding, "verify_process_binding", verify)
    monkeypatch.setattr(binding.psutil, "Process", lambda pid: process)
    monkeypatch.setattr(binding, "current_windows_lab_api", lambda: api)
    monkeypatch.setattr(binding, "Path", PureWindowsPath)
    return config, model, engine, values


def test_process_evidence_does_not_expose_observed_credentials(process_binding):
    config, model, engine, values = process_binding
    values["args"].extend(["--api-key", "fixture-secret"])
    result = binding.verify_process(config, model, engine, "127.0.0.1", 8080)
    assert "fixture-secret" not in str(result)
