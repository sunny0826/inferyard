"""Dispatch frozen request construction and idle provenance by adapter capability."""


def request_body(config, prompt, *, stream=True):
    from inferyard.registry import adapter_factory

    return adapter_factory(config["engine"]["adapter"]).request_body(config, prompt, stream=stream)


def idle_evidence(adapter, state, observation):
    result = {"state": state, "source": getattr(adapter, "idle_source", "/slots:is_processing")}
    if result["source"] == "/lab/v1/lifecycle":
        result["lab_snapshot"] = observation
    return result
