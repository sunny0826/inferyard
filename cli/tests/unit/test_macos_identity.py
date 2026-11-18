"""Synthetic macOS evidence and failure paths; no model or native service required."""

import json
import subprocess
import sys
from copy import deepcopy
from types import SimpleNamespace

import pytest

from inferyard.contracts.environment_admission import mismatches
from inferyard.errors import PreflightError
from inferyard.platforms import (
    identity,
    macos_identity,
    macos_native,
    macos_process,
    telemetry,
)
from inferyard.platforms.identity import hash_file


class NativeError(Exception):
    pass


class Gone(NativeError):
    pass


class Denied(NativeError):
    pass


@pytest.fixture
def native(monkeypatch, tmp_path):
    model = tmp_path / "model.gguf"
    model.write_bytes(b"synthetic-model")
    engine = tmp_path / "server"
    engine.write_bytes(b"synthetic-engine")
    args = [str(engine), "--model", str(model), "-ngl", "0"]
    process = SimpleNamespace(
        _proc=SimpleNamespace(create_time=lambda *, monotonic: 1_700_000_000.125),
        cmdline=lambda: args,
        exe=lambda: str(engine),
        cwd=lambda: str(tmp_path),
        memory_info=lambda: SimpleNamespace(rss=12345),
        environ=lambda: {"LLAMA_SERVER_SLOTS_DEBUG": "1", "PRIVATE_TOKEN": "do-not-persist"},
    )
    psutil = SimpleNamespace(
        Process=lambda pid: process,
        Error=NativeError,
        NoSuchProcess=Gone,
        AccessDenied=Denied,
        virtual_memory=lambda: SimpleNamespace(available=16384, total=65536),
        swap_memory=lambda: SimpleNamespace(sin=32768, sout=49152),
    )
    monkeypatch.setattr(macos_identity, "psutil_module", lambda: psutil)
    monkeypatch.setattr(macos_native, "psutil_module", lambda: psutil)
    monkeypatch.setattr(
        macos_native,
        "query",
        lambda command: (
            "Mach Virtual Memory Statistics: (page size of 16384 bytes)\n"
            "Pageins: 999999.\nPageouts: 888888.\nSwapins: 2.\nSwapouts: 3.\n"
        ),
    )
    monkeypatch.setitem(sys.modules, "psutil", psutil)
    config = {
        "endpoint": {"server_pid": 321, "process_start_ticks": 1_700_000_000_125000},
        "engine": {"startup_args": args[1:]},
    }
    monkeypatch.setattr(macos_identity, "lsof_records", lambda *args: [listener()])
    monkeypatch.setattr(macos_identity, "mapped_file", lambda *args: True)
    return psutil, process, config, hash_file(model), hash_file(engine)


def listener(**changes):
    return {
        "p": 321,
        "f": "5",
        "t": "IPv4",
        "P": "TCP",
        "T": "ST=LISTEN",
        "n": "127.0.0.1:8080",
        **changes,
    }


def test_unknown_opt_in_never_hides_observed_mismatch_or_offline_unknown():
    conditions = {
        "ac_online": True,
        "profile": "unknown",
        "governor": "unknown",
        "epp": "unknown",
        "allow_unknown_environment": True,
    }
    environment = {"platform": "Darwin", "ac_online": True}
    assert mismatches(conditions, environment) == []
    assert mismatches(conditions, {**environment, "governor": "powersave"}) == ["governor"]
    assert mismatches(conditions, {**environment, "ac_online": False}) == ["ac_online"]
    assert mismatches(conditions, {**environment, "ac_online": None}) == ["ac_online"]
    assert "profile" in mismatches({**conditions, "allow_unknown_environment": False}, environment)
    from inferyard.analysis.environment import assess_environment

    assessment = assess_environment(environment, environment, [], conditions, [])
    assert not assessment["stable_observed_environment"]
    assert "swap_counter_unknown:pswpin" in assessment["reasons"]


def test_metal_library_must_be_manifest_bound_and_actually_mapped(native, monkeypatch, tmp_path):
    _, process, config, model, engine = native
    config = deepcopy(config)
    config["engine"]["backend"] = "metal"
    with pytest.raises(PreflightError, match="metal_offload_configuration_unverified"):
        macos_identity.verify_process(config, model, engine, "127.0.0.1", 8080)
    config["engine"]["startup_args"][-1] = "1"
    process.cmdline = lambda: [engine.path, *config["engine"]["startup_args"]]
    manifest = tmp_path / "libraries.json"
    manifest.write_text("{}")
    config["engine"]["runtime_library_manifest"] = str(manifest)
    library = tmp_path / "libggml-metal.dylib"
    library.write_bytes(b"synthetic-metal-library")
    with pytest.raises(PreflightError, match="library_not_bound"):
        macos_identity.verify_process(config, model, engine, "127.0.0.1", 8080)
    manifest.write_text(json.dumps({library.name: hash_file(library).sha256}))
    monkeypatch.setattr(macos_identity, "mapped_file", lambda pid, file, rows: file == engine)
    with pytest.raises(PreflightError, match="library_not_loaded"):
        macos_identity.verify_process(config, model, engine, "127.0.0.1", 8080)
    monkeypatch.setattr(macos_identity, "mapped_file", lambda *args: True)
    result = macos_identity.verify_process(config, model, engine, "127.0.0.1", 8080)
    assert result["gpu_backend"]["source"] == "lsof:txt:file_identity"
    assert result["gpu_backend"]["gpu_residency"] == "not_observed"
    bound = [model, engine, hash_file(library)]
    monkeypatch.setattr(macos_identity, "hash_file", lambda *args: pytest.fail("guard rehashed"))
    manifest.unlink()
    result = macos_identity.verify_process(
        config, model, engine, "127.0.0.1", 8080, bound_files=bound
    )
    assert result["gpu_backend"]["library_sha256"] == bound[-1].sha256
    library.write_bytes(b"changed")
    with pytest.raises(PreflightError, match="identity_file_changed"):
        macos_identity.verify_process(config, model, engine, "127.0.0.1", 8080, bound_files=bound)
