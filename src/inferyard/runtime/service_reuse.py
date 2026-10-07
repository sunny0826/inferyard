"""Clean process reuse and replacement isolation for explicit reruns/handoffs."""

from inferyard.platforms.identity import PreflightError


def same_process(old, new):
    return all(old[key] == new[key] for key in ("server_pid", "process_start_ticks"))


def reuse_allowed(previous, config):
    old = previous["config"]
    return (
        not config["execution"].get("require_fresh_process", False)
        and not old["execution"].get("require_fresh_process", False)
        and all(old[section] == config[section] for section in ("model", "engine", "generation"))
        and all(
            old["conditions"].get(key) == config["conditions"].get(key)
            for key in ("context_size", "threads", "threads_batch", "cache_policy")
        )
        and old["endpoint"]["url"] == config["endpoint"]["url"]
    )


def require_transition(previous, config, process_start, *, reason, serial_continuation=False):
    old, new = previous["config"]["endpoint"], config["endpoint"]
    if same_process(old, new):
        if not reuse_allowed(previous, config):
            raise PreflightError(reason)
        if not serial_continuation and not previous.get("service_drain"):
            raise PreflightError("service_reuse_drain_evidence_missing")
        return "same_process"
    try:
        alive = process_start(old["server_pid"]) == old["process_start_ticks"]
    except PreflightError as exc:
        if str(exc) != "service_process_unavailable":
            raise
        alive = False
    if alive:
        raise PreflightError("previous_service_still_alive")
    return "replaced_process"


def snapshot(store, previous, config, lock, *, serial_continuation=False):
    if previous is None:
        return
    reused = same_process(previous["config"]["endpoint"], config["endpoint"])
    if reused and not serial_continuation and lock.state and lock.state.get("dirty"):
        raise PreflightError("dirty_service_requires_bound_recovery")
    if reused and not serial_continuation and not previous.get("service_drain"):
        raise PreflightError("service_reuse_drain_evidence_missing")
    store.snapshot(
        "service-reuse.json",
        {
            "definition": "service-reuse.v1",
            "previous_run_id": previous["run"]["run_id"],
            "transition": "same_process" if reused else "replaced_process",
            "serial_continuation": serial_continuation,
            "cache_state": "unknown",
            "warmup_count": config["execution"]["warmup_count"],
            "fresh_probe_required": True,
            "previous_drain": previous.get("service_drain") if reused else None,
        },
    )
