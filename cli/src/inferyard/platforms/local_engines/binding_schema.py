"""Closed local binding structure, independent of historical binding parsers."""

import ipaddress
from urllib.parse import urlsplit

from inferyard.contracts.schemas_common import HASH, LABEL, NAT, POS, VERSION, array, enum, obj
from inferyard.contracts.validation import _validate
from inferyard.platforms.identity import PreflightError

MODEL = obj(
    {
        "engine": enum("llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio"),
        "path": LABEL,
        "device": NAT,
        "inode": POS,
        "source": enum(
            "verified_startup_directory_identity",
            "verified_startup_file_identity",
            "lms_loaded_instance_path",
        ),
    }
)
OBSERVER = obj(
    {"kind": enum("lms"), "path": LABEL, "sha256": HASH, "models_root": LABEL, "instance_id": LABEL}
)
SERVICE = obj(
    {
        "pid": POS,
        "start_ticks": POS,
        "origin": LABEL,
        "address": LABEL,
        "port": {"type": "integer", "minimum": 1, "maximum": 65535},
        "executable_sha256": HASH,
        "argv_sha256": HASH,
        "cwd_sha256": HASH,
        "listener_inode": {"type": "null"},
        "listener_identity": LABEL,
        "listener_source": enum("lsof:TCP:LISTEN:pid"),
        "model_binding": MODEL,
        "workers": array(
            obj(
                {
                    "pid": POS,
                    "parent_pid": POS,
                    "start_ticks": POS,
                    "executable_sha256": HASH,
                    "argv_sha256": HASH,
                    "cwd_sha256": HASH,
                }
            )
        ),
    }
)
SERVICE["properties"]["observer"] = OBSERVER
BINDING = obj(
    {
        "schema_version": VERSION,
        "definition": enum("local-engine-binding.v1"),
        "installation_sha256": HASH,
        "service": SERVICE,
    }
)


def validate(binding, config):
    _validate(binding, BINDING, "local_engine_binding")
    service, engine, endpoint = binding["service"], config["engine"], config["endpoint"]
    origin = endpoint["url"]
    parsed = urlsplit(origin)
    address = service["address"] if parsed.hostname == "localhost" else parsed.hostname
    port = parsed.port or (443 if parsed.scheme == "https" else 80)
    try:
        if not ipaddress.ip_address(address).is_loopback:
            raise PreflightError("local_engine_binding_invalid")
    except ValueError as exc:
        raise PreflightError("local_engine_binding_invalid") from exc
    if (
        binding["installation_sha256"] != engine["installation_sha256"]
        or service["pid"] != endpoint["server_pid"]
        or service["start_ticks"] != endpoint["process_start_ticks"]
        or (service["origin"], service["address"], service["port"]) != (origin, address, port)
        or service["model_binding"]["engine"] != engine["family"]
        or service["model_binding"]["path"] != config["model"]["local_path"]
        or ("observer" in service) != (engine["family"] == "lmstudio")
    ):
        raise PreflightError("local_engine_binding_invalid")
    if "observer" in service and service["observer"]["instance_id"] != engine["served_model"]:
        raise PreflightError("local_engine_binding_invalid")
    workers = service["workers"]
    identifiers = [row["pid"] for row in workers]
    if identifiers != sorted(set(identifiers)) or service["pid"] in identifiers:
        raise PreflightError("local_engine_binding_invalid")
    parents = {row["pid"]: row["parent_pid"] for row in workers}
    for pid in identifiers:
        seen = set()
        while pid != service["pid"]:
            if pid in seen or pid not in parents:
                raise PreflightError("local_engine_binding_invalid")
            seen.add(pid)
            pid = parents[pid]
    return service
