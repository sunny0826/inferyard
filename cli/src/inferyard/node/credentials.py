"""Restricted node credential storage with no private key material in config JSON."""

from __future__ import annotations

import json
import os
import subprocess
import tempfile
from pathlib import Path

from inferyard.node.tls import normalize_fingerprint, spki_sha256, der_from_pem

CONFIG_NAME = "agent_config.json"
_SECRET_NAMES = ("ca.pem", "client.pem", "client.key", CONFIG_NAME)


def default_state_dir() -> Path:
    configured = os.environ.get("INFERYARD_NODE_STATE_DIR")
    return Path(configured).expanduser() if configured else Path.home() / ".inferyard" / "node"


def windows_acl_command(path: Path, sid: str) -> list[str]:
    if not sid or any(character in sid for character in "\r\n\x00"):
        raise ValueError("invalid Windows user SID")
    return [
        "icacls",
        str(path),
        "/inheritance:r",
        "/grant:r",
        f"*{sid}:(OI)(CI)F",
    ]


def current_windows_sid(*, runner=subprocess.run) -> str:
    completed = runner(
        ["whoami", "/user", "/fo", "csv", "/nh"],
        check=True,
        capture_output=True,
        text=True,
        timeout=10,
    )
    first = completed.stdout.splitlines()[0]
    fields = [part.strip().strip('"') for part in first.split(",")]
    if len(fields) < 2 or not fields[1].startswith("S-"):
        raise ValueError("Windows user SID unavailable")
    return fields[1]


def secure_directory(path: Path) -> Path:
    path = path.expanduser().absolute()
    path.mkdir(parents=True, exist_ok=True, mode=0o700)
    if path.is_symlink() or not path.is_dir():
        raise OSError("node state directory must be a real directory")
    if os.name != "nt":
        os.chmod(path, 0o700)
    return path


def _write_private(path: Path, content: bytes) -> None:
    fd, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    temporary_path = Path(temporary)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        if os.name != "nt":
            os.chmod(temporary_path, 0o600)
        os.replace(temporary_path, path)
        if os.name != "nt":
            os.chmod(path, 0o600)
    finally:
        temporary_path.unlink(missing_ok=True)


def _restrict_windows(paths: list[Path], *, runner=subprocess.run, sid: str | None = None) -> None:
    sid = sid or current_windows_sid(runner=runner)
    for path in paths:
        runner(windows_acl_command(path, sid), check=True, capture_output=True, timeout=15)


def save_pairing(
    state_dir: Path,
    response: dict,
    *,
    ca_sha256: str,
    autostart_mode: str,
    runner=subprocess.run,
) -> dict:
    required = ("node_id", "ca_cert_pem", "client_cert_pem", "client_key_pem")
    if any(not isinstance(response.get(key), str) or not response[key] for key in required):
        raise ValueError("approved pairing response is missing credentials")
    pinned = normalize_fingerprint(ca_sha256)
    if spki_sha256(der_from_pem(response["ca_cert_pem"])) != pinned:
        raise ValueError("returned CA certificate does not match pinned SPKI")
    hubs = response.get("hub_addrs")
    if not isinstance(hubs, list) or not hubs or any(not isinstance(item, str) for item in hubs):
        raise ValueError("approved pairing response is missing hub_addrs")
    root = secure_directory(state_dir)
    paths = [root / name for name in _SECRET_NAMES]
    if any(path.exists() or path.is_symlink() for path in paths):
        raise FileExistsError("node credentials already exist")
    config = {
        "schema_version": 1,
        "node_id": response["node_id"],
        "display_name": response.get("display_name"),
        "hub_addrs": hubs,
        "ca_sha256": pinned,
        "ca_file": "ca.pem",
        "client_cert_file": "client.pem",
        "client_key_file": "client.key",
        "autostart_mode": autostart_mode,
        "allowed_tunnel_ports": [],
    }
    try:
        _write_private(root / "ca.pem", response["ca_cert_pem"].encode("utf-8"))
        _write_private(root / "client.pem", response["client_cert_pem"].encode("utf-8"))
        _write_private(root / "client.key", response["client_key_pem"].encode("utf-8"))
        _write_private(root / CONFIG_NAME, (json.dumps(config, indent=2) + "\n").encode())
        if os.name == "nt":
            _restrict_windows([root, *paths], runner=runner)
    except BaseException:
        for path in paths:
            path.unlink(missing_ok=True)
        raise
    return config


def load_config(state_dir: Path) -> dict:
    root = secure_directory(state_dir)
    path = root / CONFIG_NAME
    if path.is_symlink():
        raise OSError("node config must not be a symlink")
    config = json.loads(path.read_text(encoding="utf-8"))
    if config.get("schema_version") != 1 or not isinstance(config.get("node_id"), str):
        raise ValueError("invalid node agent config")
    if not isinstance(config.get("hub_addrs"), list) or not config["hub_addrs"]:
        raise ValueError("node agent config has no hub addresses")
    for name in ("ca.pem", "client.pem", "client.key"):
        item = root / name
        if item.is_symlink() or not item.is_file():
            raise OSError(f"missing node credential: {name}")
        if os.name != "nt":
            os.chmod(item, 0o600)
    return config


def remove_credentials(state_dir: Path) -> None:
    root = secure_directory(state_dir)
    for name in _SECRET_NAMES:
        path = root / name
        if path.is_symlink():
            raise OSError("refusing to remove symlinked node credentials")
        path.unlink(missing_ok=True)
