"""Synthetic runtime/process checks: no model requests or service startup."""

import copy
import os
from types import SimpleNamespace

import pytest

from inferyard.config.local_engines.launch import startup
from inferyard.errors import ContractError, PreflightError
from inferyard.platforms.identity import hash_file
from inferyard.platforms.local_engines import installation as runtime
from inferyard.platforms.local_engines import service
from inferyard.platforms.local_engines.startup import check_environment, validate_startup


@pytest.fixture
def installation(tmp_path, monkeypatch):
    monkeypatch.setattr(runtime, "darwin", lambda: None)
    root = tmp_path / "runtime"
    root.mkdir()
    executable = root / "server"
    executable.write_bytes(b"synthetic executable")
    (root / "package.py").write_text("value = 1\n")
    value = {
        "schema_version": 3,
        "definition": "local-engine-installation.v1",
        "family": "llama-cpp",
        "executable": str(executable),
        "working_directory": str(tmp_path),
        "runtime_roots": [str(root)],
        "environment": {},
    }
    return value, root


@pytest.mark.parametrize("mutation", ["modify", "add", "delete", "pyc", "symlink", "same_mtime"])
def test_runtime_drift(installation, mutation):
    declaration, root = installation
    snapshot = runtime.freeze_installation(declaration)
    source = root / "package.py"
    if mutation == "modify":
        source.write_text("different\n")
    elif mutation == "add":
        (root / "injected.py").write_text("injected\n")
    elif mutation == "delete":
        source.unlink()
    elif mutation == "pyc":
        (root / "package.pyc").write_bytes(b"old unchecked bytecode")
    elif mutation == "symlink":
        source.unlink()
        source.symlink_to(root / "server")
    else:
        before = source.stat()
        source.write_text("value = 2\n")
        os.utime(source, ns=(before.st_atime_ns, before.st_mtime_ns))
    with pytest.raises(PreflightError, match="runtime_changed"):
        runtime.verify_installation(snapshot)


@pytest.fixture
def bound(tmp_path, monkeypatch):
    executable, model = tmp_path / "python", tmp_path / "model"
    executable.write_bytes(b"interpreter")
    model.mkdir()
    declaration = {
        "family": "vllm",
        "executable": str(executable),
        "working_directory": str(tmp_path),
        "environment": {},
    }
    args, _ = startup(declaration, model, "fixture", 8192, 8000)
    config = {
        "engine": {
            "family": "vllm",
            "startup_args": args,
            "environment": {},
            "working_directory": str(tmp_path),
            "installation_sha256": "a" * 64,
        },
        "model": {"local_path": str(model)},
        "conditions": {"context_size": 8192},
        "endpoint": {"server_pid": 42, "process_start_ticks": 123, "url": "http://127.0.0.1:8000"},
    }
    argv = [str(executable), *args]
    state = (123, argv, b"\0".join(x.encode() for x in argv) + b"\0", executable, tmp_path)
    psutil = SimpleNamespace(Process=lambda _: SimpleNamespace(environ=lambda: {}), Error=OSError)
    identity = hash_file(executable)
    row = {
        "f": "txt",
        "t": "REG",
        "n": str(executable),
        "p": 42,
        "D": hex(identity.device),
        "i": str(identity.inode),
    }
    monkeypatch.setattr(service.native, "service_process_state", lambda *_: state)
    monkeypatch.setattr(service, "lsof_records", lambda *_: [row])
    monkeypatch.setattr(service, "_listener_identity", lambda *_: "listener")
    monkeypatch.setattr(service.native, "unique_listener", lambda *_: None)
    return config, declaration, [identity], psutil, state
