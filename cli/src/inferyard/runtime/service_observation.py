"""Separate native serial-client admission from verified engine-wide release."""

from inferyard.adapters.requests import idle_evidence
from inferyard.platforms.identity import PreflightError


def native(adapter):
    return getattr(adapter, "observation_mode", None) == "native"


async def observe_service(adapter, seconds, store, phase="probe", key=None):
    if native(adapter):
        return await adapter.wait_ready(
            seconds,
            observe=lambda state, data: store.event("native_observed", phase, key, data),
        )
    return await adapter.wait_idle(
        seconds,
        observe=lambda state, data: store.event(
            "idle_observed", phase, key, idle_evidence(adapter, state, data)
        ),
    )


def clean_observed(lock, adapter, store, *, run_id=None, key=None, recovery=None):
    if not native(adapter):
        lock.clean()
        return
    if not lock.state or not lock.state.get("dirty"):
        return
    if recovery and recovery["old_process_gone"]:
        lock.write(
            {
                **lock.state,
                "dirty": False,
                "completion_scope": "process_replacement",
                "engine_internal_drain": None,
            }
        )
        return
    state = adapter.last_state
    if (
        run_id is not None
        and lock.state["run_id"] == run_id
        and lock.state["request_id"] == key
        and state is not None
        and state.done
        and state.finish_reason in ("stop", "length")
        and not adapter.native_uncertain
        and (
            getattr(adapter, "local_profile", False) is False
            or getattr(adapter, "queue_ready", False)
        )
    ):
        # This receipt finishes only the owned, validated HTTP roundtrip.
        store.observe(
            "native_clean_receipt",
            {
                "run_id": run_id,
                "request_id": key,
                "client_request_id": state.request_id,
                "dirty_token": lock.state["dirty_token"],
                "completion_scope": "client_http",
                "basis": "validated_http_response_and_closed_context",
                "finish_reason": state.finish_reason,
                "engine_internal_drain": None,
                "missing_reason": "native_lifecycle_unavailable",
            },
            stage="client_complete",
            key=key,
        )
        lock.write(
            {
                **lock.state,
                "dirty": False,
                "completion_scope": "client_http",
                "engine_internal_drain": None,
            }
        )
        return
    raise PreflightError("native_dirty_requires_bound_recovery")


def observation_manifest(adapter):
    value = getattr(adapter, "capability_evidence", None) if adapter else None
    return {"engine_observation": value} if value is not None else {}
