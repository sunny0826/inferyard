"""Synthetic runtime/process checks: no model requests or service startup."""

import os
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.config.local_engines.launch import startup
from inferyard.contracts.validation import json_bytes
from inferyard.errors import PreflightError
from inferyard.platforms.identity import hash_file
from inferyard.platforms.local_engines import installation as runtime
from inferyard.platforms.local_engines import service


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


def _bound_config(tmp_path):
    working = tmp_path.resolve()
    executable, model = working / "python", working / "model"
    executable.write_bytes(b"interpreter")
    model.mkdir()
    weights = model / "weights.bin"
    weights.write_bytes(b"weights")
    declaration = {
        "family": "vllm",
        "executable": str(executable),
        "working_directory": str(working),
        "environment": {},
    }
    args, _ = startup(declaration, model, "fixture", 8192, 8000)
    config = {
        "engine": {
            "family": "vllm",
            "startup_args": args,
            "environment": {},
            "working_directory": str(working),
            "installation_path": str(working / "runtime.json"),
            "installation_sha256": "a" * 64,
        },
        "model": {
            "kind": "directory",
            "local_path": str(model),
            "assets_path": str(working / "assets.json"),
            "assets_sha256": "b" * 64,
            "content_sha256": "c" * 64,
            "template_path": str(working / "template.jinja"),
            "template_sha256": "d" * 64,
        },
        "conditions": {"context_size": 8192},
        "endpoint": {
            "server_pid": 42,
            "process_start_ticks": 123,
            "url": "http://127.0.0.1:8000",
            "binding_path": str(working / "binding.json"),
        },
    }
    return config, declaration, executable, weights


def _service_stubs(monkeypatch, executable, args, working):
    state = (123, [str(executable), *args], b"", executable, working)
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
    monkeypatch.setattr(service, "darwin", lambda: None)
    monkeypatch.setattr(service.native, "psutil_module", lambda: psutil)
    monkeypatch.setattr(service, "lsof_records", lambda *_: [row])
    monkeypatch.setattr(service, "observe_workers", lambda *_: [])
    monkeypatch.setattr(service.native, "unique_listener", lambda *_: None)
    return state, identity


@pytest.fixture
def bound(tmp_path, monkeypatch):
    """Synthetic bound vLLM service whose asset scan is counted, not executed."""
    config, declaration, executable, weights = _bound_config(tmp_path)
    working = Path(config["engine"]["working_directory"])
    state, runtime_identity = _service_stubs(
        monkeypatch, executable, config["engine"]["startup_args"], working
    )
    asset_identities, calls, drift = [hash_file(weights)], {"assets": 0}, {"state": False}

    def fake_assets(audited):
        calls["assets"] += 1
        if calls["assets"] > 1 and drift.get("assets_error"):
            raise PreflightError(drift["assets_error"])
        engine = {"family": audited["engine"]["family"], **declaration}
        return {"installation": engine}, [runtime_identity], list(asset_identities)

    def process_state(*_):
        if drift["state"]:
            raise OSError
        return state

    monkeypatch.setattr(service, "assets", fake_assets)
    monkeypatch.setattr(service, "_ASSET_CACHE", {})
    monkeypatch.setattr(service.native, "service_process_state", process_state)
    monkeypatch.setattr(service, "_listener_identity", lambda *_: drift.get("listener", "listener"))

    def write_binding():
        runtime_value, runtime_files, _ = fake_assets(config)
        binding = service._bind(config, runtime_value, runtime_files)
        path = Path(config["endpoint"]["binding_path"])
        path.write_bytes(json_bytes(binding))
        config["endpoint"]["binding_sha256"] = hash_file(path).sha256
        return binding

    return SimpleNamespace(
        config=config,
        identities=[runtime_identity, *asset_identities],
        calls=calls,
        drift=drift,
        weights=weights,
        write_binding=write_binding,
    )


def test_guard_reuses_verified_assets_within_one_run(bound):
    bound.write_binding()
    bound.calls["assets"] = 0
    for _ in range(3):
        service.verify_bound_service(bound.config, bound_files=bound.identities)
    assert bound.calls["assets"] == 1


def test_binding_entry_never_reuses_cached_asset_scan(bound):
    service.bind_service(bound.config)
    service.bind_service(bound.config)
    assert bound.calls["assets"] == 2


def test_guard_rejects_changed_bound_file_after_warm_cache(bound):
    bound.write_binding()
    bound.calls["assets"] = 0
    service.verify_bound_service(bound.config, bound_files=bound.identities)
    bound.weights.write_bytes(b"changed")
    with pytest.raises(PreflightError, match="local_engine_bound_file_changed"):
        service.verify_bound_service(bound.config, bound_files=bound.identities)
    assert bound.calls["assets"] == 1


def test_guard_rejects_changed_service_binding_after_warm_cache(bound):
    bound.write_binding()
    bound.calls["assets"] = 0
    service.verify_bound_service(bound.config, bound_files=bound.identities)
    bound.drift["listener"] = "different listener"
    with pytest.raises(PreflightError, match="local_engine_bound_service_changed"):
        service.verify_bound_service(bound.config, bound_files=bound.identities)
    assert bound.calls["assets"] == 1


def test_guard_reports_unavailable_service_after_warm_cache(bound):
    bound.write_binding()
    bound.calls["assets"] = 0
    service.verify_bound_service(bound.config, bound_files=bound.identities)
    bound.drift["state"] = True
    with pytest.raises(PreflightError, match="local_engine_service_unavailable"):
        service.verify_bound_service(bound.config, bound_files=bound.identities)
    assert bound.calls["assets"] == 1


def test_guard_rescans_assets_when_the_model_directory_changes(bound):
    bound.write_binding()
    bound.calls["assets"] = 0
    service.verify_bound_service(bound.config, bound_files=bound.identities)
    bound.weights.parent.joinpath("injected.bin").write_bytes(b"injected")
    bound.drift["assets_error"] = "local_engine_model_assets_changed"
    with pytest.raises(PreflightError, match="local_engine_model_assets_changed"):
        service.verify_bound_service(bound.config, bound_files=bound.identities)
    assert bound.calls["assets"] == 2
