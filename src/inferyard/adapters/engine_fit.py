"""Bounded, local-only diagnostic transport for externally managed inference engines.

This protocol provides observations, not independent effective-parameter verification.
SGLang metrics use colon names (observability/metrics_collector.py); its legacy
/get_server_info alias still exposes the top-level version field.
"""

from __future__ import annotations

import asyncio
import codecs
import math
import re
import time
from contextlib import asynccontextmanager

import httpx

from inferyard.adapters.engine_fit_common import (
    COMMON_METRICS,
    common_service,
    observer_idle,
    observer_service,
)
from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.platforms.identity import PreflightError, resolve_loopback_origin

MAX_RESPONSE_BYTES = 8 * 1024**2
INSPECTION_TIMEOUT_SECONDS = 10.0
_METRICS = {
    "vllm": ("vllm:num_requests_running", "vllm:num_requests_waiting"),
    "sglang": ("sglang:num_running_reqs", "sglang:num_queue_reqs"),
    **COMMON_METRICS,
}
_OPTIONAL_METRICS = {
    "vllm": ("vllm:num_requests_swapped",),
    "sglang": ("sglang:num_grammar_queue_reqs",),
}
_LABEL = re.compile(r'\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*"((?:[^"\\\n]|\\[\\"n])*)"\s*')
_SAMPLE = re.compile(r"([a-zA-Z_:][a-zA-Z0-9_:]*)(?:\{(.*)\})?[ \t]+(\S+)(?:[ \t]+(\S+))?")


class FitTransportError(RuntimeError):
    """A stable reason only; remote bodies and exception details are never included."""

    def __init__(self, reason: str):
        self.reason = reason
        super().__init__(reason)


def _json(text: str) -> dict:
    try:
        value = strict_json_loads(text)
        pending = [value]
        while pending:
            item = pending.pop()
            if isinstance(item, str):
                item.encode("utf-8", "strict")
            elif isinstance(item, dict):
                pending.extend(item.keys())
                pending.extend(item.values())
            elif isinstance(item, list):
                pending.extend(item)
    except ContractError, ValueError:
        raise FitTransportError("invalid_json") from None
    if not isinstance(value, dict):
        raise FitTransportError("invalid_json_object")
    if "error" in value:
        raise FitTransportError("service_error")
    return value


def _labels(raw: str | None) -> tuple:
    values = {}
    remaining = raw or ""
    while remaining.strip():
        match = _LABEL.match(remaining)
        if not match or match[1] in values:
            raise FitTransportError("invalid_metrics_labels")
        # Prometheus escapes are narrower than JSON escapes.
        values[match[1]] = re.sub(r'\\([\\"n])', lambda m: "\n" if m[1] == "n" else m[1], match[2])
        remaining = remaining[match.end() :]
        if remaining:
            if not remaining.startswith(","):
                raise FitTransportError("invalid_metrics_labels")
            remaining = remaining[1:]
    return tuple(sorted(values.items()))


def _idle_metrics(engine: str, text: str) -> dict:
    required = _METRICS[engine]
    relevant = required + _OPTIONAL_METRICS.get(engine, ())
    samples = {}
    label_sets = {name: set() for name in relevant}
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        prefix = re.match(r"[a-zA-Z_:][a-zA-Z0-9_:]*", line)
        if not prefix or prefix[0] not in relevant:
            continue
        match = _SAMPLE.fullmatch(line)
        if not match:
            raise FitTransportError("invalid_metrics_sample")
        name, raw_labels, raw_value, timestamp = match.groups()
        labels = _labels(raw_labels)
        identity = (name, labels)
        if identity in samples:
            raise FitTransportError("duplicate_metrics_series")
        try:
            value = float(raw_value)
            valid_time = timestamp is None or math.isfinite(float(timestamp))
        except ValueError:
            raise FitTransportError("invalid_metrics_value") from None
        if not math.isfinite(value) or value < 0 or not value.is_integer() or not valid_time:
            raise FitTransportError("invalid_metrics_value")
        samples[identity] = value
        label_sets[name].add(labels)
    if any(not label_sets[name] for name in required):
        raise FitTransportError("missing_idle_metrics")
    if any(label_sets[name] != label_sets[required[0]] for name in required[1:]):
        raise FitTransportError("incomplete_idle_metrics_series")
    return {
        "idle": all(value == 0 for value in samples.values()),
        "source": "/metrics",
        "values": [
            {"metric": name, "labels": dict(labels), "value": value}
            for (name, labels), value in sorted(samples.items())
        ],
    }


class _StreamState:
    def __init__(self, started_ns: int):
        self.started_ns = started_ns
        self.first_ns = None
        self.text = []
        self.finish_reason = None
        self.done = False
        self.usage = None

    def accept(self, data: str):
        if self.done:
            raise FitTransportError("event_after_done")
        if data == "[DONE]":
            if self.finish_reason is None:
                raise FitTransportError("missing_finish_reason")
            self.done = True
            return
        event = _json(data)
        usage = event.get("usage")
        if usage is not None:
            if not isinstance(usage, dict):
                raise FitTransportError("invalid_usage")
            for key in ("prompt_tokens", "completion_tokens", "total_tokens"):
                value = usage.get(key)
                if value is not None and (type(value) is not int or value < 0):
                    raise FitTransportError("invalid_usage")
            if (
                all(
                    usage.get(key) is not None
                    for key in ("prompt_tokens", "completion_tokens", "total_tokens")
                )
                and usage["total_tokens"] != usage["prompt_tokens"] + usage["completion_tokens"]
            ):
                raise FitTransportError("inconsistent_usage")
            observed = {key: usage.get(key) for key in ("prompt_tokens", "completion_tokens")}
            if self.usage is not None and self.usage != observed:
                raise FitTransportError("inconsistent_usage")
            self.usage = observed
        choices = event.get("choices")
        if not isinstance(choices, list) or len(choices) > 1:
            raise FitTransportError("invalid_choices")
        if not choices:
            if usage is None:
                raise FitTransportError("empty_stream_event")
            return
        choice = choices[0]
        if (
            not isinstance(choice, dict)
            or type(choice.get("index")) is not int
            or choice["index"] != 0
        ):
            raise FitTransportError("invalid_choice_index")
        if self.finish_reason is not None:
            raise FitTransportError("choice_after_finish")
        delta = choice.get("delta")
        if not isinstance(delta, dict):
            raise FitTransportError("invalid_delta")
        for key in ("content", "reasoning_content", "reasoning"):
            if delta.get(key) is not None and not isinstance(delta[key], str):
                raise FitTransportError("invalid_content")
        if delta.get("tool_calls") or delta.get("function_call"):
            raise FitTransportError("unsupported_tool_response")
        content = delta.get("content") or ""
        if content:
            if self.first_ns is None:
                self.first_ns = time.monotonic_ns()
            self.text.append(content)
        finish = choice.get("finish_reason")
        if finish is not None:
            if finish not in ("stop", "length"):
                raise FitTransportError("unsupported_finish_reason")
            self.finish_reason = finish

    def result(self) -> dict:
        if not self.done:
            raise FitTransportError("missing_done")
        usage = self.usage or {"prompt_tokens": None, "completion_tokens": None}
        missing = None
        if any(value is None for value in usage.values()):
            missing = (
                "endpoint_usage_missing" if self.usage is None else "endpoint_usage_incomplete"
            )
        return {
            "text": "".join(self.text),
            "finish_reason": self.finish_reason,
            "elapsed_ms": (time.monotonic_ns() - self.started_ns) / 1e6,
            "first_content_ms": (
                None if self.first_ns is None else (self.first_ns - self.started_ns) / 1e6
            ),
            **usage,
            "usage_missing_reason": missing,
        }


async def _chunks(response):
    received = 0
    async for chunk in response.aiter_bytes():
        received += len(chunk)
        if received > MAX_RESPONSE_BYTES:
            raise FitTransportError("response_too_large")
        # Buffered transports must also allow deadline/cancellation callbacks to run.
        await asyncio.sleep(0)
        yield chunk


async def _events(response):
    decoder = codecs.getincrementaldecoder("utf-8")("strict")
    buffer = ""
    data = []
    try:
        async for chunk in _chunks(response):
            buffer += decoder.decode(chunk)
            lines = buffer.split("\n")
            buffer = lines.pop()
            for line in lines:
                line = line.removesuffix("\r")
                if not line:
                    if data:
                        await asyncio.sleep(0)
                        yield "\n".join(data)
                        data.clear()
                elif line.startswith("data:"):
                    data.append(line[5:].removeprefix(" "))
                elif not line.startswith((":", "event:", "id:", "retry:")):
                    raise FitTransportError("invalid_sse_field")
        buffer += decoder.decode(b"", final=True)
    except UnicodeError:
        raise FitTransportError("invalid_utf8") from None
    if buffer or data:
        raise FitTransportError("incomplete_sse_event")


class FitClient:
    def __init__(
        self, engine, origin, served_model, api_key=None, *, transport=None, observer=None
    ):
        if engine == "ollama":
            raise FitTransportError("ollama_service_idle_observation_unavailable")
        if engine not in (*_METRICS, "lmstudio"):
            raise FitTransportError("unsupported_engine")
        if engine == "lmstudio" and (
            not callable(getattr(observer, "inspect", None))
            or not callable(getattr(observer, "idle", None))
        ):
            raise FitTransportError("lmstudio_observer_required")
        if engine != "lmstudio" and observer is not None:
            raise FitTransportError("unexpected_engine_observer")
        if not isinstance(served_model, str) or not served_model.strip():
            raise FitTransportError("invalid_served_model")
        if api_key is not None and (
            not isinstance(api_key, str)
            or not api_key
            or any(not 33 <= ord(c) <= 126 for c in api_key)
        ):
            raise FitTransportError("invalid_api_key")
        try:
            resolved, _, _ = resolve_loopback_origin(origin)
        except PreflightError, ValueError, OSError, TypeError, AttributeError:
            raise FitTransportError("invalid_loopback_origin") from None
        self.engine = engine
        self.served_model = served_model
        self._observer = observer
        headers = {"Accept-Encoding": "identity"}
        if api_key is not None:
            headers["Authorization"] = f"Bearer {api_key}"
        self._client = httpx.AsyncClient(
            base_url=resolved,
            headers=headers,
            trust_env=False,
            follow_redirects=False,
            transport=transport,
        )

    async def __aenter__(self):
        await self._client.__aenter__()
        return self

    async def __aexit__(self, *args):
        return await self._client.__aexit__(*args)

    @asynccontextmanager
    async def _response(self, method, path, timeout, **kwargs):
        deadline = time.monotonic() + timeout
        try:
            async with asyncio.timeout(timeout):
                async with self._client.stream(method, path, timeout=timeout, **kwargs) as response:
                    if response.status_code != 200:
                        raise FitTransportError("http_status_error")
                    # Avoid unbounded allocation inside compression decoders.
                    if response.headers.get("content-encoding", "identity").lower() != "identity":
                        raise FitTransportError("unsupported_content_encoding")
                    yield response
                    if time.monotonic() > deadline:
                        raise FitTransportError("request_timeout")
        except TimeoutError, httpx.TimeoutException:
            raise FitTransportError("request_timeout") from None
        except httpx.HTTPError:
            raise FitTransportError("http_transport_error") from None

    async def _read(self, path, *, as_json=True):
        headers = {"Accept": "application/json" if as_json else "text/plain; version=0.0.4"}
        async with self._response(
            "GET", path, INSPECTION_TIMEOUT_SECONDS, headers=headers
        ) as response:
            body = bytearray()
            async for chunk in _chunks(response):
                body.extend(chunk)
        try:
            text = body.decode("utf-8")
        except UnicodeError:
            raise FitTransportError("invalid_utf8") from None
        return _json(text) if as_json else text

    async def inspect(self) -> dict:
        listing = await self._read("/v1/models")
        models = listing.get("data")
        if not isinstance(models, list) or any(
            not isinstance(model, dict) or not isinstance(model.get("id"), str) for model in models
        ):
            raise FitTransportError("invalid_models_response")
        ids = [model["id"] for model in models]
        if (
            ids.count(self.served_model) != 1
            if self.engine == "lmstudio"
            else ids != [self.served_model]
        ):
            raise FitTransportError("served_model_unverified")
        if self.engine == "lmstudio":
            return observer_service(
                await self._observe("inspect"), self.served_model, FitTransportError
            )
        if self.engine in COMMON_METRICS:
            path = "/props" if self.engine == "llama-cpp" else "/version"
            return common_service(
                self.engine, await self._read(path), self.served_model, FitTransportError
            )
        path = "/version" if self.engine == "vllm" else "/get_server_info"
        version = (await self._read(path)).get("version")
        if not isinstance(version, str) or not re.fullmatch(
            r"[0-9][A-Za-z0-9.+_-]{0,127}", version
        ):
            raise FitTransportError("engine_version_unverified")
        return {
            "engine": self.engine,
            "version": version,
            "served_model": self.served_model,
            "version_source": path,
        }

    async def idle(self) -> dict:
        if self.engine == "lmstudio":
            return observer_idle(await self._observe("idle"), self.served_model, FitTransportError)
        return _idle_metrics(self.engine, await self._read("/metrics", as_json=False))

    async def _observe(self, method):
        try:
            async with asyncio.timeout(INSPECTION_TIMEOUT_SECONDS):
                return await getattr(self._observer, method)()
        except TimeoutError:
            raise FitTransportError("request_timeout") from None
        except PreflightError, OSError, TypeError, ValueError:
            raise FitTransportError("lmstudio_observation_unavailable") from None

    async def complete(self, prompt, max_tokens, timeout_seconds) -> dict:
        if not isinstance(prompt, str) or not prompt.strip():
            raise FitTransportError("invalid_prompt")
        if type(max_tokens) is not int or max_tokens < 1:
            raise FitTransportError("invalid_max_tokens")
        if (
            type(timeout_seconds) not in (int, float)
            or not math.isfinite(timeout_seconds)
            or timeout_seconds <= 0
        ):
            raise FitTransportError("invalid_timeout")
        payload = {
            "model": self.served_model,
            "messages": [{"role": "user", "content": prompt}],
            "max_tokens": max_tokens,
            "temperature": 0,
            "stream": True,
            "stream_options": {"include_usage": True},
        }
        state = _StreamState(time.monotonic_ns())
        async with self._response(
            "POST",
            "/v1/chat/completions",
            timeout_seconds,
            json=payload,
            headers={"Accept": "text/event-stream"},
        ) as response:
            if (
                response.headers.get("content-type", "").split(";")[0].strip()
                != "text/event-stream"
            ):
                raise FitTransportError("invalid_sse_content_type")
            async for data in _events(response):
                state.accept(data)
        return state.result()
