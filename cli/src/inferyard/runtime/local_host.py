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
    service = identity["local_engine"]["binding"]["service"]
    current = {
        "definition": "local-host-service.v1",
        "scope_sha256": sha256(json_bytes(scope)).hexdigest(),
        "service": service,
    }
    state = getattr(lock, "state", None)
    previous = lock.local_service or (state.get("local_service") if state else None)
    if (
        previous is not None
        and config["engine"].get("family") == "lmstudio"
        and previous.get("scope_sha256") != current["scope_sha256"]
        and previous.get("service", {}).get("pid") == service["pid"]
        and previous.get("service", {}).get("start_ticks") == service["start_ticks"]
    ):
        raise PreflightError("local_scope_requires_new_service_process")
    lock.local_service = current
    if state is not None and hasattr(lock, "write"):
        lock.write({**state, "local_service": current})
