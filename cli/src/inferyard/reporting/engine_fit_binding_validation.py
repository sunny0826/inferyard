"""Platform-specific service bindings without importing live platform collectors."""

import ipaddress
from urllib.parse import urlsplit

from inferyard.config.engine_fit_native_sources import (
    WINDOWS_LISTENER_SOURCE,
    WINDOWS_START_SOURCE,
)
from inferyard.reporting.engine_fit_validation_primitives import (
    absolute as _absolute,
)
from inferyard.reporting.engine_fit_validation_primitives import (
    digest as _digest,
)
from inferyard.reporting.engine_fit_validation_primitives import (
    fields,
    number,
    require,
    text,
)


def validate_binding(value, plan, engine, *, macos, windows=False):
    fields(
        value,
        {
            "pid",
            "start_ticks",
            "origin",
            "address",
            "port",
            "executable_sha256",
            "argv_sha256",
            "listener_inode",
            "model_binding",
        }
        | ({"listener_identity", "listener_source", "cwd_sha256"} if macos or windows else set())
        | ({"process_start_source"} if windows else set())
        | ({"entrypoint_sha256"} if engine == "mlx-lm" else set())
        | ({"observer"} if engine == "lmstudio" else set()),
        "binding",
    )
    for key in ("pid", "start_ticks", "port"):
        require(number(value[key], integer=True) and value[key] > 0, "binding_" + key)
    require(value["port"] <= 65535, "binding_port")
    require(_digest(value["executable_sha256"]) and _digest(value["argv_sha256"]), "binding_hash")
    if macos or windows:
        require(_digest(value["cwd_sha256"]), "cwd_hash")
        require(value["listener_inode"] is None, "listener_inode")
        expected_source = WINDOWS_LISTENER_SOURCE if windows else "lsof:TCP:LISTEN:pid"
        require(
            value["listener_source"] == expected_source,
            "listener_source",
        )
        require(
            value["listener_identity"]
            == (
                f"{'windows' if windows else 'macos'}:tcp:{value['address']}:{value['port']}"
                f":pid:{value['pid']}"
            ),
            "listener_identity",
        )
    else:
        require(
            text(value["listener_inode"]) and value["listener_inode"].isdigit(), "listener_inode"
        )
    if windows:
        require(value["process_start_source"] == WINDOWS_START_SOURCE, "process_start_source")
    require(text(value["origin"]) and text(value["address"]), "binding_origin")
    try:
        address = ipaddress.ip_address(value["address"])
        origin = urlsplit(value["origin"])
        valid = (
            address.is_loopback
            and origin.scheme in ("http", "https")
            and origin.hostname == value["address"]
            and origin.port == value["port"]
            and not origin.username
            and not origin.password
            and not origin.path
            and not origin.query
            and not origin.fragment
        )
    except ValueError:
        valid = False
    require(valid, "binding_origin")
    model = value["model_binding"]
    fields(model, {"engine", "path", "device", "inode", "source"}, "model_binding")
    require(model["engine"] == engine and model["path"] == plan["model"]["path"], "model_binding")
    expected_source = {
        "llama-cpp": "verified_startup_file_identity",
        "lmstudio": "lms_loaded_instance_path",
    }.get(engine, "verified_startup_directory_identity")
    require(model["source"] == expected_source, "model_binding_source")
    if engine == "mlx-lm":
        require(_digest(value["entrypoint_sha256"]), "entrypoint_hash")
    if engine == "lmstudio":
        observer = value["observer"]
        fields(observer, {"kind", "path", "sha256", "models_root", "instance_id"}, "observer")
        require(observer["kind"] == "lms" and _digest(observer["sha256"]), "observer_identity")
        require(_absolute(observer["path"]) and _absolute(observer["models_root"]), "observer_path")
        require(text(observer["instance_id"]), "observer_instance")
    if windows:
        require(
            type(model["device"]) is int
            and 0 <= model["device"] <= 2**64 - 1
            and type(model["inode"]) is int
            and 0 <= model["inode"] <= 2**128 - 1,
            "model_binding_identity",
        )
    else:
        require(
            number(model["device"], integer=True) and number(model["inode"], integer=True),
            "model_binding_identity",
        )
