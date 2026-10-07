"""Extract reuse evidence from this read's validated, sealed events only."""

from inferyard.adapters.lab_observation import is_idle


def _idle(data, latest, observation):
    if data["state"] != "idle":
        return False
    if data["source"] == "/slots:is_processing" and observation is None:
        return True
    if (
        observation
        and observation["mode"] == "lab"
        and data["source"] == "/lab/v1/lifecycle"
        and "lab_snapshot" in data
    ):
        snapshot = data["lab_snapshot"]
        request = snapshot["request"]
        return is_idle(snapshot["lifecycle"]) and (
            request["phase"] == "released" if request else latest is None
        )
    return False


def service_drain(events, *, sealed, truncated, observation):
    if not sealed or truncated or (observation and observation["mode"] == "native"):
        return None  # Native client HTTP completion is not engine-wide drain.
    latest, proof = None, None
    for event in events:
        kind, data = event["event_type"], event["data"]
        if kind == "request_started":
            latest, proof = event["request_id"], None
        elif kind == "idle_observed":
            proof = None
            if event["request_id"] == latest and _idle(data, latest, observation):
                proof = {
                    "event_seq": event["seq"],
                    "request_id": latest,
                    "source": data["source"],
                    "completion_scope": "engine_idle",
                }
    return proof
