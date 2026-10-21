"""Persist local scope ownership across independent CLI invocations."""

from hashlib import sha256

from inferyard.evidence.storage import json_bytes
from inferyard.platforms.identity import PreflightError


def require_gone(service, process_start):
    processes = [
        (service["pid"], service["start_ticks"]),
        *((item["pid"], item["start_ticks"]) for item in service["workers"]),
    ]
    for pid, started in processes:
        try:
            alive = process_start(pid) == started
        except PreflightError as exc:
            if str(exc) != "service_process_unavailable":
                raise
            alive = False
        if alive:
            raise PreflightError("previous_local_process_still_alive")


def admit(lock, config, bundle, identity, process_start):
    scope = {key: config[key] for key in ("model", "engine", "generation", "conditions")}
    scope["bundle"] = bundle
    current = {
        "definition": "local-host-service.v1",
        "scope_sha256": sha256(json_bytes(scope)).hexdigest(),
        "service": identity["local_engine"]["binding"]["service"],
    }
    previous = (lock.state or {}).get("local_service")
    if previous is not None:
        old, new = previous["service"], current["service"]
        if (old["pid"], old["start_ticks"]) == (new["pid"], new["start_ticks"]):
            if previous["scope_sha256"] != current["scope_sha256"]:
                raise PreflightError("local_scope_requires_new_service_process")
        else:
            require_gone(old, process_start)
    lock.local_service = current
