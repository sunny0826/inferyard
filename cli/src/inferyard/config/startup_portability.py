"""Canonical startup fingerprint, allowing only explicit local binding replacement."""

import hashlib
import ipaddress
from pathlib import Path

from inferyard.evidence.storage import EvidenceError, json_bytes

BINDINGS = {
    "-m": "model",
    "--model": "model",
    "--chat-template-file": "template",
    "--host": "host",
    "--port": "port",
}


def portable_startup(config):
    args = config["engine"]["startup_args"]
    normalized, seen = [], set()
    index = 0
    while index < len(args):
        argument = args[index]
        flag, separator, inline = argument.partition("=")
        binding = BINDINGS.get(flag)
        if binding is None:
            normalized.append(argument)
            index += 1
            continue
        if binding in seen:
            raise EvidenceError("duplicate_startup_binding")
        seen.add(binding)
        if separator:
            value = inline
        else:
            index += 1
            if index >= len(args):
                raise EvidenceError("missing_startup_binding")
            value = args[index]
        if not value or value.startswith("-"):
            raise EvidenceError("invalid_startup_binding")
        if binding in ("model", "template"):
            key = "local_path" if binding == "model" else "template_path"
            if Path(value).resolve() != Path(config["model"][key]).resolve():
                raise EvidenceError("startup_artifact_binding_mismatch")
        if binding == "host":
            try:
                if value != "localhost" and not ipaddress.ip_address(value).is_loopback:
                    raise ValueError
            except ValueError as exc:
                raise EvidenceError("startup_host_not_loopback") from exc
        if binding == "port" and (
            not value.isascii() or not value.isdigit() or not 0 < int(value) < 65536
        ):
            raise EvidenceError("invalid_startup_port")
        normalized.extend(["binding:" + binding, "<local>"])
        index += 1
    if "model" not in seen:
        raise EvidenceError("missing_startup_model_binding")
    return {
        "normalization": "local-bindings.v1",
        "sha256": hashlib.sha256(json_bytes(normalized)).hexdigest(),
        "replaceable_bindings": sorted(seen),
        "artifact_bytes_verified": False,
    }
