"""Formal Windows binding refusal tests through a fake process/native file interface."""

from pathlib import PureWindowsPath
from types import SimpleNamespace

import pytest

import inferyard.platforms.lab_identity as binding
from inferyard.config.loader import load_config
from inferyard.platforms.identity import PreflightError
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
    calls = []

    def verify(expected, **kwargs):
        calls.append(expected)
        return {**expected, "listener_identity": "fixture", "details": kwargs["inspect_details"]()}

    monkeypatch.setattr(binding, "verify_process_binding", verify)
    monkeypatch.setattr(binding.psutil, "Process", lambda pid: process)
    monkeypatch.setattr(binding, "current_windows_lab_api", lambda: api)
    # Exercise native path semantics on this non-Windows unit-test host.
    monkeypatch.setattr(binding, "Path", PureWindowsPath)
    return config, model, engine, values, calls


def test_lab_process_keeps_full_filetime_and_rechecks_binding(process_binding):
    config, model, engine, _, calls = process_binding
    result = binding.verify_process(config, model, engine, "127.0.0.1", 8080)
    assert result["start_ticks"] == config["endpoint"]["process_start_ticks"]
    assert result["model_binding"] == "verified_startup_native_file_identity"
    assert len(calls) == 1


@pytest.mark.parametrize("adapter", ["kvmem", "ninfer"])
@pytest.mark.parametrize(
    "declared,loaded", [(True, False), (True, True), (False, True), (False, False)]
)
def test_cudart_declaration_requires_matching_actual_mapping(
    process_binding, adapter, declared, loaded
):
    config, model, engine, values, _ = process_binding
    config["engine"]["adapter"] = adapter
    path = r"D:\lab\cudart64_13.dll"
    if declared:
        config["engine"]["asset_manifest"].append(
            {"path": path, "role": "library", "bytes": 12, "sha256": "c" * 64}
        )
    if loaded:
        values["maps"].append(SimpleNamespace(path=path))
    if declared != loaded:
        reason = "lab_library_not_loaded" if declared else "lab_unbound_engine_library"
        with pytest.raises(PreflightError, match=reason):
            binding.verify_process(config, model, engine, "127.0.0.1", 8080)
    else:
        assert (
            binding.verify_process(config, model, engine, "127.0.0.1", 8080)["binary"] == "verified"
        )


def test_ninfer_positional_asset_and_frozen_cuda_environment(process_binding):
    config, model, engine, values, calls = process_binding
    config["engine"]["adapter"] = "ninfer"
    config["engine"]["startup_args"].pop(0)  # Native release takes the model positionally.
    values["args"] = [engine.path, *config["engine"]["startup_args"]]
    config["engine"]["environment"] = {"CUDA_VISIBLE_DEVICES": "0", "CUDA_CACHE_DISABLE": "1"}
    values["env"] = dict(config["engine"]["environment"])
    assert binding.verify_process(config, model, engine, "127.0.0.1", 8080)["binary"] == "verified"
    assert len(calls) == 1
    values["env"]["CUDA_VISIBLE_DEVICES"] = "1"
    with pytest.raises(PreflightError, match="lab_unbound_engine_environment"):
        binding.verify_process(config, model, engine, "127.0.0.1", 8080)


@pytest.mark.parametrize(
    "failure,reason",
    [
        ("model", "service_model_argument_mismatch"),
        ("startup", "service_startup_arguments_mismatch"),
        ("environment", "lab_unbound_engine_environment"),
        ("unloaded", "lab_library_not_loaded"),
        ("unbound", "lab_unbound_engine_library"),
        ("reused", "lab_windows_pid_reused"),
    ],
)
def test_lab_process_refuses_unbound_or_changed_identity(
    process_binding, monkeypatch, failure, reason
):
    config, model, engine, values, _ = process_binding
    if failure == "model":
        values["inode"] = 3
    elif failure == "startup":
        values["args"].append("--unbound")
    elif failure == "environment":
        values["env"]["CUDA_CACHE_DISABLE"] = "1"
    elif failure == "unloaded":
        values["maps"].clear()
    elif failure == "unbound":
        values["maps"].append(SimpleNamespace(path=r"D:\lab\unbound.dll"))
    elif failure == "reused":

        def reused(*args, **kwargs):
            kwargs["inspect_details"]()
            raise PreflightError("lab_windows_pid_reused")

        monkeypatch.setattr(binding, "verify_process_binding", reused)
    with pytest.raises(PreflightError, match=reason):
        binding.verify_process(config, model, engine, "127.0.0.1", 8080)


@pytest.mark.parametrize("key", ["LLAMA_TEST", "GGML_TEST", "KVMEM_TEST", "NINFER_TEST", "PATH"])
def test_environment_outside_contract_identity_surface_is_not_frozen(process_binding, key):
    config, model, engine, values, calls = process_binding
    values["env"][key] = "fixture-override"
    result = binding.verify_process(config, model, engine, "127.0.0.1", 8080)
    assert result["engine_environment"] == "verified_cuda_device_cache_overrides"
    assert "fixture-override" not in str(result)
    assert len(calls) == 1


def test_process_evidence_uses_independent_sanitized_observed_argv(process_binding):
    config, model, engine, values, calls = process_binding
    values["args"].extend(["--api-key", "fixture-secret"])
    result = binding.verify_process(config, model, engine, "127.0.0.1", 8080)
    assert result["startup_args"] == binding.sanitized_arguments(values["args"][1:])
    assert result["startup_args"] is not config["engine"]["startup_args"]
    assert "fixture-secret" not in str(result)
    assert calls[0]["argv_sha256"] == binding.argv_digest(
        [engine.path, *config["engine"]["startup_args"]]
    )


def test_bind_files_uses_one_size_scaled_deadline(process_binding, monkeypatch):
    config, *_ = process_binding
    entries = config["engine"]["asset_manifest"]
    for entry in entries:
        entry["bytes"] = 1024**3
    started = 100
    budget = binding.manifest_hash_seconds(entries)
    assert budget > 180
    seen = []

    def verify(rows, *, deadline):
        seen.append(deadline)
        # Simulate a scan that used to exceed 180 seconds without allocating large files.
        assert started + 181 < deadline == started + budget
        return [{**row, "native_stamp": [1, "01" * 16, 0, row["bytes"], 0, 3, 0]} for row in rows]

    monkeypatch.setattr(binding.time, "monotonic", lambda: started)
    monkeypatch.setattr(binding, "verify_file_manifest", verify)
    monkeypatch.setattr(binding, "read_json", lambda path: {"engine.dll": "d" * 64})
    monkeypatch.setattr("inferyard.platforms.lab_assets.inspect_asset", lambda config: None)
    monkeypatch.setattr(binding.LabFileIdentity, "unchanged", lambda self: True)
    result = binding.bind_files(config)
    assert len(result) == len(entries) and seen == [started + budget]
