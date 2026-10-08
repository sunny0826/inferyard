import hashlib
import os
import sys
from pathlib import Path

import pytest

from inferyard.platforms.identity import (
    PreflightError,
    check_resources,
    hash_file,
    memory_available,
    process_start_ticks,
    verify_listener,
    verify_process,
)


def test_hash_change_and_replacement_are_detected(tmp_path):
    path = tmp_path / "model"
    path.write_bytes(b"model-v1")
    digest = hashlib.sha256(b"model-v1").hexdigest()
    identity = hash_file(path, digest)
    path.write_bytes(b"model-v2")
    assert not identity.unchanged()
    with pytest.raises(PreflightError, match="hash_mismatch"):
        hash_file(path, digest)


@pytest.fixture
def proc_service(tmp_path):
    if os.name == "nt":
        pytest.skip(
            "Linux inode/device and /proc socket fixtures; Windows has native identity tests"
        )
    proc = tmp_path / "proc"
    directory = proc / "123"
    (directory / "fd").mkdir(parents=True)
    (directory / "net").mkdir()
    model = tmp_path / "model.gguf"
    model.write_bytes(b"synthetic model")
    binary = tmp_path / "server"
    binary.write_bytes(b"synthetic executable")
    (directory / "exe").symlink_to(binary)
    fields = ["S"] + ["0"] * 18 + ["67890"] + ["0"] * 10
    (directory / "stat").write_text("123 (server with ) spaces) " + " ".join(fields))
    (directory / "cmdline").write_bytes(b"\0".join([b"server", b"-ngl", b"0", b""]))
    item = model.stat()
    (directory / "maps").write_text(
        f"0000-1000 r--p 00000000 {os.major(item.st_dev):02x}:{os.minor(item.st_dev):02x} "
        f"{item.st_ino} {model}\n"
    )
    (directory / "fd/5").symlink_to("socket:[456]")
    (directory / "net/tcp").write_text(
        "sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n"
        "0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 456\n"
    )
    config = {
        "endpoint": {"server_pid": 123, "process_start_ticks": 67890},
        "engine": {"startup_args": ["-ngl", "0"]},
    }
    return proc, config, hash_file(model), hash_file(binary)


def test_endpoint_pid_binary_and_model_are_jointly_checked(proc_service):
    proc, config, model, engine = proc_service
    assert process_start_ticks(123, proc) == 67890
    identity = verify_process(config, model, engine, "127.0.0.1", 8080, proc)
    assert identity["endpoint"] == identity["model_mapping"] == "verified"
    with pytest.raises(PreflightError, match="endpoint_pid_mismatch"):
        verify_listener(123, "127.0.0.1", 8081, proc)
    config["endpoint"]["process_start_ticks"] = 67891
    with pytest.raises(PreflightError, match="identity_changed"):
        verify_process(config, model, engine, "127.0.0.1", 8080, proc)


@pytest.mark.parametrize("change", ["binary", "mapping", "arguments", "listener"])
def test_wrong_identity_components_block(proc_service, change):
    proc, config, model, engine = proc_service
    if change == "binary":
        engine = model
    elif change == "mapping":
        (proc / "123/maps").write_text("")
    elif change == "arguments":
        config["engine"]["startup_args"] = []
    else:
        (proc / "123/fd/5").unlink()
    with pytest.raises(PreflightError):
        verify_process(config, model, engine, "127.0.0.1", 8080, proc)


def test_resource_limits_and_memavailable_source(tmp_path):
    (tmp_path / "meminfo").write_text("MemTotal: 999999999 kB\nMemAvailable: 8388608 kB\n")
    assert memory_available(tmp_path) == 8 * 1024**3
    config = {"output": {"min_available_memory_bytes": 8 * 1024**3, "min_disk_bytes": 5 * 1024**3}}
    check_resources(config, 8 * 1024**3, 5 * 1024**3)
    with pytest.raises(PreflightError, match="memory"):
        check_resources(config, 7 * 1024**3, 5 * 1024**3)
    with pytest.raises(PreflightError, match="disk"):
        check_resources(config, 8 * 1024**3, 4 * 1024**3)
    (tmp_path / "meminfo").write_text("MemTotal: 999999999 kB\n")
    with pytest.raises(PreflightError, match="unavailable"):
        memory_available(tmp_path)


@pytest.mark.skipif(sys.platform != "linux", reason="Native Linux /proc process identity")
def test_real_proc_start_ticks_can_be_read():
    assert process_start_ticks(os.getpid(), Path("/proc")) > 0


def test_unmapped_model_requires_matching_startup_inode(proc_service):
    proc, config, model, engine = proc_service
    directory = proc / "123"
    (directory / "maps").write_text("")
    with pytest.raises(PreflightError, match="mapping_unverified"):
        verify_process(config, model, engine, "127.0.0.1", 8080, proc)
    config["engine"]["startup_args"] += ["-m", model.path]
    (directory / "cmdline").write_bytes(
        b"\0".join(s.encode() for s in ["server", *config["engine"]["startup_args"], ""])
    )
    result = verify_process(config, model, engine, "127.0.0.1", 8080, proc)
    assert result["model_mapping"] == "not_retained"
    assert result["model_binding"] == "verified_startup_file_inode"


@pytest.mark.parametrize("mapped", [False, True])
@pytest.mark.parametrize("flag", ["-m", "--model"])
@pytest.mark.parametrize("inline", [False, True])
def test_model_argument_forms_keep_mapping_and_inode_checks(proc_service, mapped, flag, inline):
    proc, config, model, engine = proc_service
    if not mapped:
        (proc / "123/maps").write_text("")
    args = [f"{flag}={model.path}"] if inline else [flag, model.path]
    config["engine"]["startup_args"] = args
    (proc / "123/cmdline").write_bytes("\0".join(["server", *args, ""]).encode())
    result = verify_process(config, model, engine, "127.0.0.1", 8080, proc)
    assert result["model_binding"] == ("mapped_inode" if mapped else "verified_startup_file_inode")
    assert result["model_mapping"] == ("verified" if mapped else "not_retained")
    if not mapped:
        # Same bytes at the declared path cannot substitute for the bound file identity.
        path = Path(model.path)
        path.rename(path.with_suffix(".old"))
        path.write_bytes(b"synthetic model")
        with pytest.raises(PreflightError, match="service_model_argument_mismatch"):
            verify_process(config, model, engine, "127.0.0.1", 8080, proc)


@pytest.mark.parametrize(
    "args",
    [
        [],
        ["--model"],
        ["--model="],
        ["-m="],
        ["--model", "--other"],
        ["--model", "MODEL", "-m", "MODEL"],
        ["--model=MODEL", "-m=MODEL"],
        ["--model", "MODEL", "-m"],
    ],
)
def test_unmapped_model_rejects_missing_or_ambiguous_argument(proc_service, args):
    proc, config, model, engine = proc_service
    args = [value.replace("MODEL", model.path) for value in args]
    config["engine"]["startup_args"] = args
    (proc / "123/maps").write_text("")
    (proc / "123/cmdline").write_bytes("\0".join(["server", *args, ""]).encode())
    with pytest.raises(PreflightError):
        verify_process(config, model, engine, "127.0.0.1", 8080, proc)


@pytest.mark.parametrize(
    "environment,accepted",
    [
        (b"OTHER=fixture-private-value\0LLAMA_SERVER_SLOTS_DEBUG=1\0", True),
        (b"LLAMA_SERVER_SLOTS_DEBUG=0\0", False),
        (b"OTHER=1\0", False),
        (b"LLAMA_SERVER_SLOTS_DEBUG=1\0LLAMA_SERVER_SLOTS_DEBUG=1\0", False),
    ],
)
def test_declared_debug_flag_must_match_actual_service_environment(
    proc_service, environment, accepted
):
    proc, config, model, engine = proc_service
    config["engine"]["slots_debug"] = True
    (proc / "123/environ").write_bytes(environment)
    if accepted:
        identity = verify_process(config, model, engine, "127.0.0.1", 8080, proc)
        assert "fixture-private-value" not in repr(identity)
    else:
        with pytest.raises(PreflightError, match="slots_debug_environment_mismatch"):
            verify_process(config, model, engine, "127.0.0.1", 8080, proc)
