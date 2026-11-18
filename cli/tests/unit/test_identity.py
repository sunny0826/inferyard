import hashlib
import os
import sys
from pathlib import Path

import pytest

from inferyard.errors import PreflightError
from inferyard.platforms.identity import check_resources, hash_file, memory_available, process_start_ticks, verify_listener, verify_process


def test_adapter_rechecks_dns_after_listener_resolution(monkeypatch):
    from inferyard.adapters.prism import PrismAdapter
    from inferyard.platforms import identity

    calls = iter(("127.0.0.1", "192.0.2.1"))
    monkeypatch.setattr(
        identity.socket,
        "getaddrinfo",
        lambda *a, **k: [(None, None, None, None, (next(calls), 80))],
    )
    origin, address, _ = identity.service_origin("http://localhost")
    assert origin == "http://localhost"
    assert address == "127.0.0.1"
    with pytest.raises(identity.PreflightError, match="localhost_resolved_outside_loopback"):
        PrismAdapter(origin)


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
        "engine": {"startup_args": ["-ngl", "0", "-m", str(model)]},
    }
    (directory / "cmdline").write_bytes(
        "\0".join(["server", *config["engine"]["startup_args"], ""]).encode()
    )
    return proc, config, hash_file(model), hash_file(binary)


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
