"""Synthetic macOS/Windows model binding; native APIs are replaced by observations."""

import importlib.util
import sys
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.platforms import macos_identity
from inferyard.platforms.identity import PreflightError, hash_file


class SyntheticProcessError(Exception):
    pass


@pytest.fixture
def windows(monkeypatch):
    monkeypatch.setitem(sys.modules, "winreg", SimpleNamespace())
    path = Path(macos_identity.__file__).with_name("windows_identity.py")
    spec = importlib.util.spec_from_file_location("synthetic_windows_identity", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    monkeypatch.setattr(module, "process_start_ticks", lambda pid: 123)
    monkeypatch.setattr(module, "verify_listener", lambda *args: "synthetic_listener")
    return module


@pytest.mark.parametrize("platform", ["macos", "windows"])
@pytest.mark.parametrize("form", ["-m", "--model", "-m=", "--model="])
@pytest.mark.parametrize("change", [None, "wrong_file", "duplicate", "missing", "empty"])
def test_model_binding_keeps_native_file_identity_checks(tmp_path, windows, platform, form, change):
    path = tmp_path / "model.gguf"
    path.write_bytes(b"synthetic-model")
    model = hash_file(path)
    engine = tmp_path / "server"
    engine.write_bytes(b"synthetic-engine")
    args = [form + path.name] if form.endswith("=") else [form, path.name]
    if change == "duplicate":
        args += ["--model=" + path.name]
    elif change == "missing":
        args += ["--model"]
    elif change == "empty":
        args = ["--model="]
    elif change == "wrong_file":
        other = tmp_path / "other.gguf"
        other.write_bytes(b"synthetic-model")
        model = hash_file(other)
    process = SimpleNamespace(cwd=lambda: str(tmp_path), exe=lambda: str(engine))
    windows.psutil = SimpleNamespace(Process=lambda pid: process, Error=SyntheticProcessError)
    windows.process_arguments = lambda pid: [str(engine), *args]

    def verify():
        if platform == "macos":
            return macos_identity._model_argument(args, process, model)
        config = {
            "endpoint": {"server_pid": 321, "process_start_ticks": 123},
            "engine": {"startup_args": args},
        }
        return windows.verify_process(config, model, hash_file(engine), "127.0.0.1", 8080)

    if change:
        reason = (
            "service_model_argument_mismatch"
            if change == "wrong_file"
            else ("service_model_mapping_unverified")
        )
        with pytest.raises(PreflightError, match=reason):
            verify()
    else:
        verify()
