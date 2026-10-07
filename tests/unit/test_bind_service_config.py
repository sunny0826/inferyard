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


def test_flat_tables_and_existing_nested_policy_keep_exact_text(binder):
    module, _ = binder
    fields = {
        "name": "中文",
        "enabled": True,
        "count": 3,
        "ratio": 0.7,
        "paths": [r"D:\lab\engine.exe", "--flag"],
        "policy": {"mode": "unsupported", "enabled": False},
    }
    assert "\n".join(module.table_lines(("engine",), fields)) == (
        '\n[engine]\nname = "中文"\nenabled = true\ncount = 3\nratio = 0.7\n'
        'paths = ["D:\\\\lab\\\\engine.exe", "--flag"]\n\n[engine.policy]\n'
        'mode = "unsupported"\nenabled = false'
    )


def test_recursive_tables_arrays_and_quoted_keys_round_trip(binder):
    module, _ = binder
    fields = {
        "assets": [{"path": r"D:\模型\a.gguf", "metadata": {"file.name": {"bytes": 12}}}],
        "empty": {},
        "policy": {"files": {"cuda.dll": "a" * 64, 'quoted"name': "tab\tline\n\x7f"}},
    }
    text = "\n".join(module.table_lines(("engine",), fields))
    assert tomllib.loads(text) == {"engine": fields}


def test_flat_prism_binding_loads_and_preserves_declarations(
    binder, config_path, tmp_path, monkeypatch, capsys
):
    module, observed = binder
    out = tmp_path / "prism-bound.toml"
    original = bind(module, observed, config_path, out, monkeypatch)
    loaded = load_config(out).config.to_dict()
    assert loaded == original
    assert json.loads(capsys.readouterr().out)["config"] == str(out)
    before = out.read_bytes()
    with pytest.raises(FileExistsError):
        module.main()
    assert out.read_bytes() == before


def test_bad_serialization_is_rejected_before_output_publication(
    binder, config_path, tmp_path, monkeypatch
):
    module, observed = binder
    out = tmp_path / "bad-bound.toml"
    monkeypatch.setattr(module, "toml_value", lambda value: '{"invalid": 1}')
    with pytest.raises(tomllib.TOMLDecodeError):
        bind(module, observed, config_path, out, monkeypatch)
    assert not out.exists()
