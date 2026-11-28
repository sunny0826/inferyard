"""One-time zero-form node pairing over a pinned TLS 1.3 connection."""

from __future__ import annotations

import json
import os
import platform
import socket
import ssl
from pathlib import Path

from inferyard import __version__
from inferyard.contracts.node_protocol import (
    EVENT_HELLO,
    EVENT_PING,
    EVENT_PONG,
    HANDSHAKE_CONTROL_LINE,
    HANDSHAKE_PAIR_LINE,
    MAX_CONTROL_LINE_BYTES,
    PAIRING_OPTION_REJECT,
    encode_ndjson_frame,
    normalize_pair_code,
)
from inferyard.node.autostart import AutostartManager, installation_marker
from inferyard.node.credentials import load_config, remove_credentials, save_pairing
from inferyard.node.tls import control_context, pair_context, peer_certificates, pinned_ca_pem


class PairingError(RuntimeError):
    def __init__(self, reason: str, message: str = ""):
        super().__init__(message or reason)
        self.reason = reason


def parse_hub(value: str) -> tuple[str, int]:
    value = value.strip()
    if not value or "://" in value or any(char.isspace() for char in value):
        raise ValueError("hub must be host:port")
    if value.startswith("["):
        host, separator, port = value[1:].partition("]:")
        if not separator:
            raise ValueError("hub must be [IPv6]:port")
    else:
        host, separator, port = value.rpartition(":")
        if not separator or ":" in host:
            raise ValueError("hub must be host:port")
    try:
        number = int(port)
    except ValueError as exc:
        raise ValueError("hub port must be an integer") from exc
    if not host or not 1 <= number <= 65535:
        raise ValueError("invalid hub address")
    return host, number


def normalize_hubs(values: list[str] | tuple[str, ...] | str) -> list[str]:
    raw_values = [values] if isinstance(values, str) else values
    result = []
    for raw in raw_values:
        for value in raw.split(","):
            host, port = parse_hub(value)
            rendered = f"[{host}]:{port}" if ":" in host else f"{host}:{port}"
            if rendered not in result:
                result.append(rendered)
    if not result:
        raise ValueError("at least one hub address is required")
    return result


def device_snapshot() -> dict:
    from inferyard.platforms.device_preflight import hardware_snapshot

    try:
        return hardware_snapshot(Path.cwd())
    except (OSError, ValueError, RuntimeError) as exc:
        return {"status": "unavailable", "reason": type(exc).__name__}


def normalized_os_arch() -> tuple[str, str, str]:
    os_name = platform.system().lower()
    raw_arch = platform.machine().lower()
    arch = {
        "x86_64": "amd64",
        "amd64": "amd64",
        "aarch64": "arm64",
        "arm64": "arm64",
    }.get(raw_arch, raw_arch)
    return os_name, arch, f"{os_name}/{arch}"


def _device_fields(snapshot: dict) -> dict:
    hardware = {
        key: snapshot.get(key)
        for key in ("cpu_model", "logical_cpus", "memory_total_bytes", "gpu")
        if snapshot.get(key) is not None
    }
    os_name, arch, plat = normalized_os_arch()
    return {
        "hostname": socket.gethostname(),
        "os": os_name,
        "arch": arch,
        "platform": plat,
        "cli_version": __version__,
        "hardware_summary": json.dumps(hardware, ensure_ascii=False, separators=(",", ":")),
        "device_snapshot": snapshot,
    }


def pair_hello(code: str, *, autostart: bool, source_hint: str | None) -> dict:
    return {
        "code": normalize_pair_code(code),
        **_device_fields(device_snapshot()),
        "autostart_mode": "none" if not autostart else normalized_os_arch()[0],
        "source_hint": source_hint or "",
    }


def node_hello(config: dict, state_dir: Path) -> dict:
    return {
        "node_id": config["node_id"],
        **_device_fields(device_snapshot()),
        "autostart_installed": (state_dir / "autostart.json").exists(),
    }


def _receive_response(stream) -> dict:
    line = stream.readline(MAX_CONTROL_LINE_BYTES + 1)
    if not line or len(line) > MAX_CONTROL_LINE_BYTES or not line.endswith(b"\n"):
        raise PairingError("pairing_response_invalid")
    try:
        response = json.loads(line)
    except (UnicodeError, json.JSONDecodeError) as exc:
        raise PairingError("pairing_response_invalid") from exc
    if not isinstance(response, dict):
        raise PairingError("pairing_response_invalid")
    return response


def _pair_at(
    hub: str,
    hello: dict,
    ca_sha256: str,
    *,
    timeout: float,
) -> dict:
    host, port = parse_hub(hub)
    try:
        with socket.create_connection((host, port), timeout=timeout) as raw:
            raw.settimeout(timeout)
            with pair_context().wrap_socket(raw, server_hostname=host) as tls:
                ca_pem = pinned_ca_pem(peer_certificates(tls), ca_sha256)
                try:
                    tls.unwrap()
                except (OSError, ssl.SSLError):
                    pass
        with socket.create_connection((host, port), timeout=timeout) as raw:
            raw.settimeout(timeout)
            with pair_context(ca_pem).wrap_socket(raw, server_hostname=host) as tls:
                tls.sendall(HANDSHAKE_PAIR_LINE.encode("ascii"))
                tls.sendall(encode_ndjson_frame(hello))
                with tls.makefile("rb") as stream:
                    return _receive_response(stream)
    except (OSError, ssl.SSLError, ValueError) as exc:
        raise PairingError("pairing_connection_failed", str(exc)) from exc


def verify_paired_hub(
    state_dir: Path, hub: str, *, timeout: float = 15, config: dict | None = None
) -> None:
    host, port = parse_hub(hub)
    try:
        with socket.create_connection((host, port), timeout=timeout) as raw:
            raw.settimeout(timeout)
            context = control_context(state_dir)
            with context.wrap_socket(raw, server_hostname=host) as tls:
                tls.sendall(HANDSHAKE_CONTROL_LINE.encode("ascii"))
                hello = node_hello(config or load_config(state_dir), state_dir)
                tls.sendall(encode_ndjson_frame({"event": EVENT_HELLO, "data": hello}))
                tls.sendall(encode_ndjson_frame({"event": EVENT_PING, "data": {}}))
                with tls.makefile("rb") as stream:
                    for _ in range(8):
                        response = _receive_response(stream)
                        if response.get("event") == EVENT_PONG:
                            return
                raise PairingError("paired_hub_verification_failed", "missing control pong")
    except (OSError, ssl.SSLError) as exc:
        raise PairingError("paired_hub_verification_failed", str(exc)) from exc


def join(
    *,
    hubs: list[str] | str,
    code: str,
    ca_sha256: str,
    state_dir: Path,
    no_autostart: bool = False,
    source_hint: str | None = None,
    timeout: float = 120,
    autostart_manager=None,
) -> dict:
    candidates = normalize_hubs(hubs)
    hello = pair_hello(code, autostart=not no_autostart, source_hint=source_hint)
    errors = []
    response = None
    paired_hub = None
    for hub in candidates:
        try:
            response = _pair_at(hub, hello, ca_sha256, timeout=timeout)
            paired_hub = hub
            break
        except PairingError as exc:
            errors.append({"hub": hub, "reason": exc.reason})
    if response is None:
        raise PairingError("pairing_connection_failed", json.dumps(errors))
    if response.get("decision") == PAIRING_OPTION_REJECT or response.get("ok") is False:
        raise PairingError("pair_rejected", str(response.get("message") or "pairing rejected"))
    if response.get("ok") is not True or response.get("decision") != "approve_pair":
        raise PairingError("pairing_response_invalid")
    config = save_pairing(
        state_dir,
        response,
        ca_sha256=ca_sha256,
        autostart_mode=hello["autostart_mode"],
    )
    try:
        verify_paired_hub(state_dir, paired_hub or candidates[0], config=config)
    except BaseException:
        remove_credentials(state_dir)
        raise
    result = {
        "node_id": config["node_id"],
        "state_dir": str(state_dir),
        "hub_addrs": config["hub_addrs"],
        "autostart": {"mode": "none"},
    }
    if not no_autostart:
        manager = autostart_manager or AutostartManager(state_dir)
        result["autostart"] = manager.install()
        installation_marker(state_dir, result["autostart"])
    return result
