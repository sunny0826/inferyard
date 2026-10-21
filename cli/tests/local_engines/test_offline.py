"""Canonical frozen artifacts can be verified after all local assets disappear."""

import copy
import hashlib
from dataclasses import asdict
from pathlib import Path

import pytest

from inferyard.config.local_engines.launch import startup
from inferyard.contracts.validation import ContractError
from inferyard.platforms.identity import PreflightError, hash_file
from inferyard.platforms.local_engines.assets import assets, bound_json
from inferyard.platforms.local_engines.offline import validate_frozen_identity
from tests.local_engine_runtime.identity import freeze
from tests.local_engine_transport.conftest import rig  # noqa: F401
from tests.local_engines.test_assets import setup_assets, write_json
from tests.unit.test_local_engine_contract import local_config  # noqa: F401


def sealed(tmp_path, monkeypatch):
    config, model, manifest = setup_assets(tmp_path, monkeypatch)
    runtime, _, identities = assets(config)
    engine = config["engine"]
    declaration = runtime["installation"]
    arguments, _ = startup(declaration, model, "fixture", 8192, 8000)
    engine.update(environment={}, working_directory=str(tmp_path), startup_args=arguments)
    config["conditions"] = {"context_size": 8192}
    raw = (
        b"\0".join(arg.encode() for arg in [declaration["executable"], *engine["startup_args"]])
        + b"\0"
    )
    service = {
        "pid": 42,
        "start_ticks": 123,
        "origin": "http://127.0.0.1:8000",
        "address": "127.0.0.1",
        "port": 8000,
        "executable_sha256": hash_file(Path(declaration["executable"])).sha256,
        "argv_sha256": hashlib.sha256(raw).hexdigest(),
        "cwd_sha256": hashlib.sha256(str(tmp_path).encode()).hexdigest(),
        "workers": [],
        "listener_inode": None,
        "listener_identity": "macos:tcp:127.0.0.1:8000:pid:42",
        "listener_source": "lsof:TCP:LISTEN:pid",
        "model_binding": {
            "engine": "llama-cpp",
            "path": str(model),
            "device": 1,
            "inode": 1,
            "source": "verified_startup_directory_identity",
        },
    }
    binding = {
        "schema_version": 3,
        "definition": "local-engine-binding.v1",
        "installation_sha256": engine["installation_sha256"],
        "service": service,
    }
    path = tmp_path / "binding.json"
    config["endpoint"] = {
        "server_pid": 42,
        "process_start_ticks": 123,
        "url": "http://127.0.0.1:8000",
        "binding_path": str(path),
        "binding_sha256": write_json(path, binding),
    }
    return (
        config,
        {"runtime": runtime, "binding": binding, "model_assets": manifest},
        [*[asdict(item) for item in identities], asdict(hash_file(path))],
    )


def test_offline_does_not_open_assets(tmp_path, monkeypatch):
    config, local, files = sealed(tmp_path, monkeypatch)
    import shutil

    shutil.rmtree(tmp_path)
    assert validate_frozen_identity(config, local, files=files)["pid"] == 42


def test_frozen_json_requires_canonical_bytes(tmp_path):
    path = tmp_path / "pretty.json"
    path.write_text('{"value": 1}')
    with pytest.raises(PreflightError, match="not_canonical"):
        bound_json(path, hash_file(path).sha256)


def test_offline_model_body_tampering(tmp_path, monkeypatch):
    config, local, files = sealed(tmp_path, monkeypatch)
    local["model_assets"]["files"][0]["sha256"] = "f" * 64
    with pytest.raises(ContractError):
        validate_frozen_identity(config, local, files=files)


def test_offline_runtime_body_tampering(tmp_path, monkeypatch):
    config, local, files = sealed(tmp_path, monkeypatch)
    local["runtime"]["roots"][0]["entries"][0]["sha256"] = "f" * 64
    with pytest.raises(ContractError):
        validate_frozen_identity(config, local, files=files)


def test_offline_file_inventory_tampering(tmp_path, monkeypatch):
    config, local, files = sealed(tmp_path, monkeypatch)
    files = copy.deepcopy(files)
    files[0]["size"] += 1
    with pytest.raises(ContractError):
        validate_frozen_identity(config, local, files=files)


@pytest.fixture
def python_sealed(local_config, rig, tmp_path):  # noqa: F811
    config = copy.deepcopy(local_config)
    state = rig("vllm")
    config["engine"]["family"] = "vllm"
    config["endpoint"].update(
        server_pid=state.binding["pid"],
        process_start_ticks=state.binding["start_ticks"],
        url=state.binding["origin"],
    )
    local, identities = freeze(config, state, tmp_path)
    return config, local, [asdict(item) for item in identities]


def refresh_documents(config, local, identities):
    from inferyard.evidence.storage import json_bytes

    argv = [
        local["runtime"]["installation"]["executable"],
        *config["engine"]["startup_args"],
    ]
    local["binding"]["service"]["argv_sha256"] = hashlib.sha256(
        b"\0".join(arg.encode() for arg in argv) + b"\0"
    ).hexdigest()
    for name, section, path_key, hash_key in (
        ("runtime", "engine", "installation_path", "installation_sha256"),
        ("binding", "endpoint", "binding_path", "binding_sha256"),
    ):
        content = json_bytes(local[name])
        digest = hashlib.sha256(content).hexdigest()
        config[section][hash_key] = digest
        if name == "runtime":
            local["binding"]["installation_sha256"] = digest
        for identity in identities:
            if identity["path"] == config[section][path_key]:
                identity.update(sha256=digest, size=len(content))


def test_offline_bootstrap_is_portable_and_does_not_read_current_files(python_sealed, monkeypatch):
    config, local, identities = python_sealed
    source = config["engine"]["startup_args"][3]
    moved = "/unavailable/copied-package/data/local_engine/bootstrap.py"
    config["engine"]["startup_args"][3] = moved
    for root in local["runtime"]["roots"]:
        if root["path"] == source:
            root["path"] = moved
    refresh_documents(config, local, identities)

    def refuse(*_):
        raise AssertionError("offline verification must not read live files")

    monkeypatch.setattr(Path, "open", refuse)
    monkeypatch.setattr(Path, "resolve", refuse)
    assert validate_frozen_identity(config, local, files=identities)["pid"] > 0


@pytest.mark.parametrize("mutation", ["bypass", "missing", "duplicate", "context"])
def test_offline_rejects_invalid_python_startup_even_with_matching_hashes(python_sealed, mutation):
    config, local, identities = python_sealed
    source = config["engine"]["startup_args"][3]
    root = next(row for row in local["runtime"]["roots"] if row["path"] == source)
    if mutation == "bypass":
        config["engine"]["startup_args"][3:5] = ["-m", "vllm.entrypoints.openai.api_server"]
    elif mutation == "missing":
        local["runtime"]["roots"].remove(root)
    elif mutation == "duplicate":
        local["runtime"]["roots"].append(copy.deepcopy(root))
    else:
        config["conditions"]["context_size"] = 128
    refresh_documents(config, local, identities)
    with pytest.raises(ContractError):
        validate_frozen_identity(config, local, files=identities)


def test_offline_worker_declaration_is_bound_without_live_reads(python_sealed, monkeypatch):
    config, local, identities = python_sealed
    declaration = local["runtime"]["installation"]
    declaration["worker_environment"] = {
        **declaration["environment"],
        "VLLM_WORKER_MULTIPROC_METHOD": "spawn",
    }
    with pytest.raises(ContractError):
        validate_frozen_identity(config, local, files=identities)
    refresh_documents(config, local, identities)

    def refuse(*_):
        raise AssertionError("worker environment verification must remain offline")

    monkeypatch.setattr(Path, "open", refuse)
    monkeypatch.setattr(Path, "resolve", refuse)
    assert validate_frozen_identity(config, local, files=identities)["pid"] > 0


@pytest.mark.parametrize(
    "mutation", ["missing_offline", "foreign_family", "unknown", "library_path"]
)
def test_offline_worker_semantics_reject_even_with_updated_hashes(python_sealed, mutation):
    config, local, identities = python_sealed
    declaration = local["runtime"]["installation"]
    worker = declaration["worker_environment"] = dict(declaration["environment"])
    if mutation == "missing_offline":
        worker.pop("HF_HUB_OFFLINE")
    elif mutation == "foreign_family":
        worker["SGLANG_USE_CUDA_IPC_TRANSPORT"] = "0"
    elif mutation == "unknown":
        worker["PYTHONPATH"] = "/unfrozen"
    else:
        worker["LD_LIBRARY_PATH"] = declaration["runtime_roots"][0]
    refresh_documents(config, local, identities)
    with pytest.raises(ContractError):
        validate_frozen_identity(config, local, files=identities)
