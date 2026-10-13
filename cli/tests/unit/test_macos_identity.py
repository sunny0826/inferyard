"""Synthetic macOS evidence and failure paths; no model or native service required."""

import json
import subprocess
import sys
from copy import deepcopy
from types import SimpleNamespace

import pytest

from inferyard.config.environment_binding import mismatches
from inferyard.platforms import (
    identity,
    macos_identity,
    macos_native,
    macos_process,
    telemetry,
)
from inferyard.platforms.identity import PreflightError, hash_file


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


def test_native_start_microseconds_and_joint_identity(native):
    _, _, config, model, engine = native
    result = macos_identity.verify_process(config, model, engine, "127.0.0.1", 8080)
    assert result["start_ticks"] == 1_700_000_000_125000
    assert result["process_start_source"] == "psutil:macos:raw_kernel_starttime:microseconds"
    assert result["model_binding"] == "verified_startup_file_inode"
    assert result["model_mapping"] == "not_observed"
    assert result["listener_inode"] is None


@pytest.mark.parametrize(
    "change,reason",
    [
        ("start", "identity_changed"),
        ("exe", "binary_mismatch"),
        ("mapping", "binary_mapping_unverified"),
        ("args", "arguments_mismatch"),
        ("model", "model_argument_mismatch"),
        ("listener", "endpoint_pid_mismatch"),
    ],
)
def test_joint_identity_rejects_wrong_components(native, monkeypatch, tmp_path, change, reason):
    _, process, config, model, engine = native
    if change == "start":
        config["endpoint"]["process_start_ticks"] += 1
    elif change == "exe":
        process.exe = lambda: model.path
    elif change == "mapping":
        # The frozen pathname now points to a replacement; the process maps the old inode.
        monkeypatch.setattr(macos_identity, "mapped_file", lambda *args: False)
    elif change == "args":
        config["engine"]["startup_args"] = []
    elif change == "model":
        other = tmp_path / "different-model"
        other.write_bytes(b"other")
        model = hash_file(other)
    else:
        monkeypatch.setattr(macos_identity, "lsof_records", lambda *args: [listener(p=999)])
    with pytest.raises(PreflightError, match=reason):
        macos_identity.verify_process(config, model, engine, "127.0.0.1", 8080)


@pytest.mark.parametrize(
    "exception,reason",
    [
        (Gone, "process_unavailable"),
        (Denied, "identity_unreadable"),
        (PermissionError, "identity_unreadable"),
    ],
)
def test_permission_is_not_process_disappearance(native, exception, reason):
    psutil, *_ = native

    def fail(pid):
        raise exception()

    psutil.Process = fail
    with pytest.raises(PreflightError, match=reason):
        macos_identity.process_start_ticks(321)


@pytest.mark.parametrize("read", ["arguments", "listener", "process", "rss"])
def test_pid_reuse_rejected_after_observation(native, monkeypatch, read):
    _, _, config, model, engine = native
    expected = config["endpoint"]["process_start_ticks"]
    calls = iter([expected, expected + 1])
    module = telemetry if read == "rss" else macos_identity
    monkeypatch.setattr(module, "process_start_ticks", lambda *args: next(calls))
    if read == "rss":
        monkeypatch.setattr(telemetry.platform, "system", lambda: "Darwin")
        assert telemetry.read_rss(321, expected) == (None, "source_changed")
    elif read == "process":
        # The fresh file-table observation has its own identity guard.
        monkeypatch.setattr(
            macos_identity,
            "process_arguments",
            lambda pid: [engine.path, *config["engine"]["startup_args"]],
        )
        with pytest.raises(PreflightError, match="identity_changed"):
            macos_identity.verify_process(config, model, engine, "127.0.0.1", 8080)
    else:
        with pytest.raises(PreflightError, match="identity_changed"):
            if read == "arguments":
                macos_identity.process_arguments(321)
            else:
                macos_identity.verify_listener(321, "127.0.0.1", 8080)


def test_slots_debug_persists_only_verified_boolean(native):
    _, process, config, model, engine = native
    config["engine"]["slots_debug"] = True
    result = macos_identity.verify_process(config, model, engine, "127.0.0.1", 8080)
    assert result["slots_debug_environment_verified"] is True
    assert "do-not-persist" not in json.dumps(result)
    process.environ = lambda: {"LLAMA_SERVER_SLOTS_DEBUG": "0"}
    with pytest.raises(PreflightError, match="slots_debug_environment_mismatch"):
        macos_identity.verify_process(config, model, engine, "127.0.0.1", 8080)


@pytest.mark.parametrize(
    "row,address,accepted",
    [
        (listener(), "127.0.0.1", True),
        (listener(n="*:8080"), "127.0.0.1", True),
        (listener(n="127.0.0.1:8081"), "127.0.0.1", False),
        (listener(n="127.0.0.2:8080"), "127.0.0.1", False),
        (listener(t="IPv6", n="[::1]:8080"), "::1", True),
        (listener(t="IPv6", n="[::1]:8080"), "127.0.0.1", False),
        (listener(T="ST=ESTABLISHED"), "127.0.0.1", False),
        (listener(P="UDP"), "127.0.0.1", False),
    ],
)
def test_listener_requires_protocol_family_address_port_and_state(row, address, accepted):
    assert macos_process.matching_listener(row, address, 8080) is accepted


def test_lsof_uses_bounded_machine_output_and_preserves_owner(monkeypatch):
    def run(command, **kwargs):
        assert command[:6] == ["/usr/sbin/lsof", "-nP", "-a", "-p", "321", "-iTCP:8080"]
        assert kwargs["timeout"] == 2
        assert kwargs.get("shell", False) is False
        return SimpleNamespace(
            returncode=0,
            stderr="",
            stdout=("p321\0\nf5\0tIPv4\0PTCP\0n127.0.0.1:8080\0TST=LISTEN\0TQR=0\0\n"),
        )

    monkeypatch.setattr(macos_process.subprocess, "run", run)
    assert macos_process.lsof_records(321, ["-iTCP:8080"])[0] == listener()


@pytest.mark.parametrize("failure", ["timeout", "permission", "stderr", "malformed"])
def test_lsof_failures_do_not_become_absence(monkeypatch, failure):
    def run(*args, **kwargs):
        if failure == "timeout":
            raise subprocess.TimeoutExpired("lsof", 2)
        if failure == "permission":
            raise PermissionError
        return SimpleNamespace(
            returncode=1 if failure == "stderr" else 0,
            stderr="denied" if failure == "stderr" else "",
            stdout="" if failure == "stderr" else "pbad\0",
        )

    monkeypatch.setattr(macos_process.subprocess, "run", run)
    with pytest.raises(PreflightError, match="listener_identity_unavailable"):
        macos_process.lsof_records(321, [])


def test_environment_is_lightweight_and_keeps_linux_fields_unknown(native, monkeypatch):
    commands = []
    monkeypatch.setattr(
        macos_identity,
        "query",
        lambda command: commands.append(command) or "Now drawing from 'AC Power'",
    )
    monkeypatch.setattr(macos_identity, "sysctl_string", lambda key: "Apple M4")
    monkeypatch.setattr(macos_identity, "boot_id", lambda: "native-boot")
    monkeypatch.setattr(
        macos_identity,
        "read_native",
        lambda: (
            True,
            {
                "source": "macos.iokit.power-policy.v1",
                "ac_online": True,
                "low_power_mode": 0,
                "power_mode": "unsupported",
                "power_mode_supported": False,
            },
        ),
    )
    monkeypatch.setattr(
        macos_identity,
        "swap_snapshot",
        lambda: {
            "pswpin": 2,
            "pswpout": 3,
            "page_size_bytes": 16384,
            "source": {
                "pswpin": "host_statistics64:swapins",
                "pswpout": "host_statistics64:swapouts",
            },
        },
    )
    snapshot = macos_identity.environment_snapshot()
    assert snapshot["platform"] == "Darwin" and snapshot["ac_online"] is True
    assert snapshot["swap_pages"] == {"pswpin": 2, "pswpout": 3}
    assert snapshot["profile"] == "macos-low-power:0"
    assert snapshot["governor"] is snapshot["epp"] is None
    assert snapshot["memory_source"] == "psutil:virtual_memory:available"
    assert commands == []


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


def test_default_darwin_dispatch_and_custom_proc_root(native, monkeypatch, tmp_path):
    monkeypatch.setattr(identity.platform, "system", lambda: "Darwin")
    assert identity.process_start_ticks(321) == 1_700_000_000_125000
    assert identity.memory_available() == 16384
    (tmp_path / "321").mkdir()
    (tmp_path / "321/stat").write_text("321 (fake) " + " ".join(["S"] + ["0"] * 18 + ["77"]))
    (tmp_path / "meminfo").write_text("MemAvailable: 8 kB\n")
    assert identity.process_start_ticks(321, tmp_path) == 77
    assert identity.memory_available(tmp_path) == 8192
    samples = telemetry.sample_memory(321, 1_700_000_000_125000, "formal", "r1")
    assert [sample["source"] for sample in samples] == [
        "psutil:virtual_memory:available",
        "psutil:Process.memory_info:rss",
    ]
    assert samples[1]["value"] == 12345


def test_swap_uses_host_counters_and_rejects_a_failed_read(native, monkeypatch):
    monkeypatch.setattr(macos_native, "_host_vm_swap", lambda: (16384, 2, 3))
    observed = macos_native.swap_snapshot()
    assert observed["pswpin"] == 2 and observed["pswpout"] == 3
    assert observed["page_size_bytes"] == 16384
    assert observed["source"] == {
        "pswpin": "host_statistics64:swapins",
        "pswpout": "host_statistics64:swapouts",
    }
    monkeypatch.setattr(macos_native, "_host_vm_swap", lambda: (16384, None, 3))
    assert macos_native.swap_snapshot()["pswpin"] is None
    monkeypatch.setattr(macos_native, "_host_vm_swap", lambda: (None, None, None))
    assert macos_native.swap_snapshot()["pswpout"] is None


def test_metal_requires_native_apple_capability(monkeypatch):
    monkeypatch.setattr(macos_identity, "sysctl_text", lambda key: "1")
    monkeypatch.setattr(
        macos_identity,
        "query",
        lambda *args, **kwargs: json.dumps(
            {
                "SPDisplaysDataType": [
                    {"sppci_model": "Apple M4", "spdisplays_metal": "spdisplays_metal3"}
                ]
            }
        ),
    )
    result = macos_identity.metal_capability()
    assert result["apple_silicon"] is True and result["gpu"]["backend"] == "metal"
    monkeypatch.setattr(macos_identity, "sysctl_text", lambda key: "0")
    with pytest.raises(PreflightError, match="metal_device_unavailable"):
        macos_identity.metal_capability()


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


def test_metal_guard_matches_a_resolved_library_symlink(native, monkeypatch, tmp_path):
    _, process, config, model, engine = native
    config = deepcopy(config)
    config["engine"]["backend"] = "metal"
    config["engine"]["startup_args"][-1] = "1"
    process.cmdline = lambda: [engine.path, *config["engine"]["startup_args"]]
    target = tmp_path / "libggml-metal.0.dylib"
    target.write_bytes(b"synthetic-metal-library")
    library = tmp_path / "libggml-metal.dylib"
    library.symlink_to(target.name)
    bound = [model, engine, hash_file(library)]
    assert bound[-1].path != str(library)
    monkeypatch.setattr(macos_identity, "hash_file", lambda *args: pytest.fail("guard rehashed"))
    result = macos_identity.verify_process(
        config, model, engine, "127.0.0.1", 8080, bound_files=bound
    )
    assert result["gpu_backend"]["library_sha256"] == bound[-1].sha256
    assert result["gpu_backend"]["loaded_library"] == bound[-1].path


def test_mapping_requires_the_captured_file_identity(native, monkeypatch):
    *_, engine = native
    row = {"p": 321, "f": "txt", "t": "REG", "D": hex(engine.device), "i": str(engine.inode)}
    monkeypatch.setattr(macos_process, "lsof_records", lambda *args: [row])
    assert macos_process.mapped_file(321, engine)
    row["i"] = str(engine.inode + 1)
    assert not macos_process.mapped_file(321, engine)


@pytest.mark.parametrize(
    "system,backend", [("Linux", "metal"), ("Windows", "metal"), ("Darwin", "cuda")]
)
def test_wrong_platform_backend_is_rejected_before_file_reads(monkeypatch, system, backend):
    monkeypatch.setattr(identity.platform, "system", lambda: system)
    with pytest.raises(PreflightError, match="unsupported_backend_platform"):
        identity.static_preflight({"engine": {"backend": backend}})


@pytest.mark.parametrize("backend", ["cpu", "metal"])
def test_static_preflight_darwin_keeps_explicit_unknown_policy(
    native, monkeypatch, tmp_path, backend
):
    _, process, config, model, engine = native
    template = tmp_path / "template.jinja"
    template.write_text("synthetic template")
    template_id = hash_file(template)
    library = tmp_path / "libggml-metal.dylib"
    library.write_bytes(b"synthetic library")
    manifest = tmp_path / "runtime.json"
    manifest.write_text(json.dumps({library.name: hash_file(library).sha256}))
    config["model"] = {
        "local_path": model.path,
        "sha256": model.sha256,
        "template_path": template_id.path,
        "template_sha256": template_id.sha256,
    }
    config["engine"].update(
        binary_path=engine.path,
        binary_sha256=engine.sha256,
        runtime_library_manifest=str(manifest),
        backend=backend,
    )
    config["endpoint"]["url"] = "http://127.0.0.1:8080"
    config["output"] = {"root": str(tmp_path), "min_available_memory_bytes": 1, "min_disk_bytes": 1}
    config["conditions"] = {
        "ac_online": True,
        "profile": "unknown",
        "governor": "unknown",
        "epp": "unknown",
        "allow_unknown_environment": True,
    }
    monkeypatch.setattr(identity.platform, "system", lambda: "Darwin")
    environment = {"platform": "Darwin", "ac_online": True, "mem_available_bytes": 65536}
    monkeypatch.setattr(identity, "environment_snapshot", lambda: dict(environment))
    if backend == "metal":
        config["engine"]["startup_args"][-1] = "1"
        process.cmdline = lambda: [engine.path, *config["engine"]["startup_args"]]
        monkeypatch.setattr(macos_identity, "mapped_file", lambda *args: True)
        monkeypatch.setattr(
            macos_identity,
            "metal_capability",
            lambda: {"apple_silicon": True, "apple_gpu": {"status": "observed", "devices": []}},
        )
    observed, _ = identity.static_preflight(config)
    assert observed["verification"] == "verified"
    assert observed["environment"].get("governor") is None
    environment["ac_online"] = False
    with pytest.raises(PreflightError, match="frozen_environment_mismatch"):
        identity.static_preflight(config)


@pytest.mark.parametrize("value", [True, -1, float("nan"), float("inf")])
def test_invalid_native_creation_time_cannot_become_an_identity(native, value):
    _, process, *_ = native
    process._proc.create_time = lambda *, monotonic: value
    with pytest.raises(PreflightError, match="identity_unreadable"):
        macos_identity.process_start_ticks(321)


def test_native_start_identity_ignores_wall_clock_adjustment(native):
    _, process, *_ = native
    expected = macos_identity.process_start_ticks(321)
    process.create_time = lambda: 1_700_000_000.125 + 3600
    assert macos_identity.process_start_ticks(321) == expected
    process._proc.create_time = lambda *, monotonic: 1_700_000_001.125
    assert macos_identity.process_start_ticks(321) != expected
