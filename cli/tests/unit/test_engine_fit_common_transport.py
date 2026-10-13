import asyncio
import copy
import json

import httpx
import pytest

from inferyard.adapters import engine_fit, engine_fit_common
from inferyard.adapters.engine_fit import FitClient, FitTransportError
from inferyard.platforms.identity import PreflightError

MODEL = "fixture-model"


class Observer:
    def __init__(self):
        self.calls = []
        self.service = {
            "engine": "lmstudio",
            "version": None,
            "served_model": MODEL,
            "version_source": "not_exposed",
            "version_missing_reason": "lmstudio_service_version_not_exposed",
        }
        self.state = {
            "idle": True,
            "source": "lms:ps",
            "values": [
                {"metric": name, "labels": {"instance": MODEL}, "value": 0}
                for name in ("lmstudio:queued", "lmstudio:active")
            ],
        }

    async def inspect(self):
        self.calls.append("inspect")
        return copy.deepcopy(self.service)

    async def idle(self):
        self.calls.append("idle")
        return copy.deepcopy(self.state)


def request(engine, method, handler, *, observer=None):
    async def run():
        async with FitClient(
            engine,
            "http://127.0.0.1:8000",
            MODEL,
            transport=httpx.MockTransport(handler),
            observer=observer,
        ) as client:
            if method == "complete":
                return await client.complete("fixture prompt", 16, 1)
            return await getattr(client, method)()

    return asyncio.run(run())


def inspecting(payload, *, ids=None):
    def handler(req):
        if req.url.path == "/v1/models":
            return httpx.Response(200, json={"data": [{"id": i} for i in ids or [MODEL]]})
        return httpx.Response(200, json=payload)

    return handler


def test_llama_build_info_is_preserved_and_props_are_not_persisted():
    seen = []

    def handler(req):
        seen.append(req.url.path)
        return inspecting({"build_info": "b8919-dc80c5252", "secret": "not-recorded"})(req)

    assert request("llama-cpp", "inspect", handler) == {
        "engine": "llama-cpp",
        "version": "b8919-dc80c5252",
        "served_model": MODEL,
        "version_source": "/props",
    }
    assert seen == ["/v1/models", "/props"]


@pytest.mark.parametrize("build_info", [None, {}, True, 8919, "0.3.0", "b8-secret value", ""])
def test_llama_version_failure_has_no_remote_body(build_info):
    with pytest.raises(FitTransportError, match="^engine_version_unverified$"):
        request("llama-cpp", "inspect", inspecting({"build_info": build_info, "secret": "value"}))


def test_llama_unknown_commit_remains_explicit_reported_build():
    assert (
        request("llama-cpp", "inspect", inspecting({"build_info": "b0-unknown"}))["version"]
        == "b0-unknown"
    )


def mlx_payload():
    return {
        "version": "0.28.3",
        "engine_fit_protocol": "mlx-lm.v1",
        "engine_fit_source_sha256": "a" * 64,
    }


def test_mlx_requires_protocol_and_current_packaged_source(monkeypatch):
    monkeypatch.setattr(engine_fit_common, "_mlx_source_sha256", lambda: "a" * 64)
    assert request("mlx-lm", "inspect", inspecting(mlx_payload())) == {
        "engine": "mlx-lm",
        "version": "0.28.3",
        "served_model": MODEL,
        "version_source": "/version",
    }


@pytest.mark.parametrize(
    "key,value",
    [
        ("engine_fit_protocol", None),
        ("engine_fit_protocol", "mlx-lm.v2"),
        ("engine_fit_source_sha256", None),
        ("engine_fit_source_sha256", "b" * 64),
        ("engine_fit_source_sha256", True),
    ],
)
def test_stock_or_stale_mlx_server_is_rejected(monkeypatch, key, value):
    monkeypatch.setattr(engine_fit_common, "_mlx_source_sha256", lambda: "a" * 64)
    with pytest.raises(FitTransportError, match="^mlx_service_protocol_unverified$"):
        request("mlx-lm", "inspect", inspecting({**mlx_payload(), key: value}))


@pytest.mark.parametrize("version", [None, 1, True, "error secret", ""])
def test_mlx_still_requires_real_package_version(monkeypatch, version):
    monkeypatch.setattr(engine_fit_common, "_mlx_source_sha256", lambda: "a" * 64)
    with pytest.raises(FitTransportError, match="^engine_version_unverified$"):
        request("mlx-lm", "inspect", inspecting({**mlx_payload(), "version": version}))


@pytest.mark.parametrize("engine", ["llama-cpp", "mlx-lm"])
def test_native_server_alias_or_extra_loaded_model_is_rejected(engine):
    with pytest.raises(FitTransportError, match="^served_model_unverified$"):
        request(engine, "inspect", inspecting({}, ids=[MODEL, "second-model"]))


@pytest.mark.parametrize("engine", ["llama-cpp", "mlx-lm"])
def test_common_metrics_include_all_running_waiting_and_poisoned_states(engine):
    names = engine_fit_common.COMMON_METRICS[engine]
    payload = "\n".join(f"{name} 0" for name in names)
    assert request(engine, "idle", lambda _: httpx.Response(200, text=payload))["idle"] is True
    for name in names:
        busy = payload.replace(f"{name} 0", f"{name} 1")
        assert (
            request(engine, "idle", lambda _, text=busy: httpx.Response(200, text=text))["idle"]
            is False
        )


@pytest.mark.parametrize("engine", ["llama-cpp", "mlx-lm"])
@pytest.mark.parametrize("broken", ["missing", "duplicate", "different_labels", "fraction"])
def test_common_metrics_fail_closed(engine, broken):
    names = engine_fit_common.COMMON_METRICS[engine]
    lines = [f"{name} 0" for name in names]
    if broken == "missing":
        lines.pop()
    elif broken == "duplicate":
        lines.append(lines[-1])
    elif broken == "different_labels":
        lines[-1] = f'{names[-1]}{{worker="1"}} 0'
    else:
        lines[-1] = f"{names[-1]} 0.5"
    with pytest.raises(FitTransportError):
        request(engine, "idle", lambda _: httpx.Response(200, text="\n".join(lines)))


def test_lmstudio_candidates_are_not_mistaken_for_loaded_instances():
    observer = Observer()
    result = request(
        "lmstudio", "inspect", inspecting({}, ids=["candidate", MODEL]), observer=observer
    )
    assert result == observer.service
    assert observer.calls == ["inspect"]


@pytest.mark.parametrize("ids", [[MODEL, MODEL], ["candidate"]])
def test_lmstudio_target_must_exist_once_before_local_observation(ids):
    observer = Observer()
    with pytest.raises(FitTransportError, match="^served_model_unverified$"):
        request("lmstudio", "inspect", inspecting({}, ids=ids), observer=observer)
    assert observer.calls == []


@pytest.mark.parametrize("observer", [None, object()])
def test_lmstudio_cannot_send_requests_without_observer(observer):
    with pytest.raises(FitTransportError, match="^lmstudio_observer_required$"):
        FitClient("lmstudio", "http://127.0.0.1:8000", MODEL, observer=observer)


@pytest.mark.parametrize("field,value", [("version", "cli-commit"), ("served_model", "other")])
def test_lmstudio_service_observation_cannot_substitute_cli_version_or_model(field, value):
    observer = Observer()
    observer.service[field] = value
    with pytest.raises(FitTransportError, match="^lmstudio_service_unverified$"):
        request("lmstudio", "inspect", inspecting({}), observer=observer)


def test_lmstudio_idle_uses_observer_only_and_retains_busy_state():
    observer = Observer()

    def no_http(_):
        pytest.fail("lms observer must not call endpoint health or model inference")

    assert request("lmstudio", "idle", no_http, observer=observer)["idle"] is True
    observer.state["values"][0]["value"] = 2
    observer.state["idle"] = False
    observed = request("lmstudio", "idle", no_http, observer=observer)
    assert observed["idle"] is False
    assert observed["source"] == "lms:ps"
    assert any(sample["value"] == 2 for sample in observed["values"])


@pytest.mark.parametrize("value", [True, -1, 0.0, 0.5, None, "0"])
def test_lmstudio_malformed_queue_count_rejected(value):
    observer = Observer()
    observer.state["values"][0]["value"] = value
    with pytest.raises(FitTransportError, match="^lmstudio_idle_unverified$"):
        request("lmstudio", "idle", inspecting({}), observer=observer)


@pytest.mark.parametrize("broken", ["missing", "duplicate", "wrong_instance", "idle_lie", "active"])
def test_lmstudio_ambiguous_or_inconsistent_idle_rejected(broken):
    observer = Observer()
    if broken == "missing":
        observer.state["values"].pop()
    elif broken == "duplicate":
        observer.state["values"][1] = observer.state["values"][0]
    elif broken == "wrong_instance":
        observer.state["values"][0]["labels"]["instance"] = "other"
    elif broken == "idle_lie":
        observer.state["idle"] = False
    else:
        observer.state["values"][1]["value"] = 2
    with pytest.raises(FitTransportError, match="^lmstudio_idle_unverified$"):
        request("lmstudio", "idle", inspecting({}), observer=observer)


@pytest.mark.parametrize("method", ["inspect", "idle"])
def test_lmstudio_observer_errors_are_bounded_and_sanitized(monkeypatch, method):
    observer = Observer()

    async def denied():
        raise PreflightError("secret observer failure")

    setattr(observer, method, denied)
    with pytest.raises(FitTransportError, match="^lmstudio_observation_unavailable$"):
        request("lmstudio", method, inspecting({}), observer=observer)

    async def slow():
        await asyncio.sleep(0.1)

    monkeypatch.setattr(engine_fit, "INSPECTION_TIMEOUT_SECONDS", 0.001)
    setattr(observer, method, slow)
    with pytest.raises(FitTransportError, match="^request_timeout$"):
        request("lmstudio", method, inspecting({}), observer=observer)


def test_ollama_is_explicitly_blocked_before_any_network_client():
    with pytest.raises(FitTransportError, match="^ollama_service_idle_observation_unavailable$"):
        FitClient("ollama", "http://127.0.0.1:11434", MODEL)


@pytest.mark.parametrize("engine", ["llama-cpp", "mlx-lm", "lmstudio"])
@pytest.mark.parametrize("terminal", ["both", "no_done", "no_finish"])
def test_common_completion_preserves_usage_and_requires_both_terminals(engine, terminal):
    event = {
        "choices": [
            {
                "index": 0,
                "delta": {"content": "北京"},
                "finish_reason": None if terminal == "no_finish" else "stop",
            }
        ],
        "usage": {"prompt_tokens": 7, "completion_tokens": 2, "total_tokens": 9},
    }
    raw = f"data: {json.dumps(event, ensure_ascii=False)}\n\n"
    if terminal != "no_done":
        raw += "data: [DONE]\n\n"

    def handler(req):
        payload = json.loads(req.content)
        assert payload["stream_options"] == {"include_usage": True}
        assert payload["temperature"] == 0 and payload["model"] == MODEL
        return httpx.Response(200, headers={"content-type": "text/event-stream"}, text=raw)

    observer = Observer() if engine == "lmstudio" else None
    if terminal == "both":
        result = request(engine, "complete", handler, observer=observer)
        assert result["text"] == "北京"
        assert result["prompt_tokens"] == 7 and result["completion_tokens"] == 2
        assert result["usage_missing_reason"] is None
    else:
        reason = "missing_done" if terminal == "no_done" else "missing_finish_reason"
        with pytest.raises(FitTransportError, match=f"^{reason}$"):
            request(engine, "complete", handler, observer=observer)


@pytest.mark.parametrize("engine", ["llama-cpp", "mlx-lm", "lmstudio"])
def test_new_engine_inspection_still_rejects_redirect_and_remote_body(engine):
    with pytest.raises(FitTransportError, match="^http_status_error$"):
        request(
            engine,
            "inspect",
            lambda _: httpx.Response(
                307, headers={"location": "https://remote.example"}, text="sensitive body"
            ),
            observer=Observer() if engine == "lmstudio" else None,
        )
