"""Persist local scope ownership across independent CLI invocations."""

from hashlib import sha256

from inferyard.errors import PreflightError
from inferyard.contracts.validation import json_bytes


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
    _ = process_start
    scope = {key: config[key] for key in ("model", "engine", "generation", "conditions")}
    scope["bundle"] = bundle
    current = {
        "definition": "local-host-service.v1",
        "scope_sha256": sha256(json_bytes(scope)).hexdigest(),
        "service": identity["local_engine"]["binding"]["service"],
    }
    lock.local_service = current
