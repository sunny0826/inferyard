"""Exercise binding output with injected Windows process observations, never a real service."""

import importlib.util
import json
import sys
import tomllib
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.config.loader import load_config


@pytest.fixture
def binder(monkeypatch):
    path = Path(__file__).parents[2] / "scripts/bind_service_config.py"
    spec = importlib.util.spec_from_file_location("service_config_binder", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    observed = {"argv": [], "ticks": 67890}
    monkeypatch.setattr(module, "os", SimpleNamespace(name="nt"))
    monkeypatch.setattr(module, "process_start_ticks", lambda pid: observed["ticks"])
    monkeypatch.setitem(
        sys.modules,
        "inferyard.platforms.windows_identity",
        SimpleNamespace(process_arguments=lambda pid: observed["argv"]),
    )
    return module, observed


def bind(module, observed, source, out, monkeypatch, *, pid=12345, endpoint=None):
    config = load_config(source).config.to_dict()
    observed["argv"] = [config["engine"]["binary_path"], *config["engine"]["startup_args"]]
    argv = [
        "bind_service_config.py",
        "--candidate",
        str(source),
        "--pid",
        str(pid),
        "--endpoint",
        endpoint or config["endpoint"]["url"],
        "--out",
        str(out),
    ]
    monkeypatch.setattr(sys, "argv", argv)
    module.main()
    return config
