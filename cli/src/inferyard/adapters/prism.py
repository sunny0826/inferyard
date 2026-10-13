"""PrismML b10743-adfffbe41 HTTP/SSE contract, verified with local protocol fixtures."""

from __future__ import annotations

import asyncio
import codecs
import hashlib
import json
import math
import time

import httpx

from inferyard.adapters.response_state import ResponseState
from inferyard.analysis.engine_timing import capture_timings
from inferyard.config.cache_policy import disabled_startup
from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.evidence.storage import EvidenceError, Redactor
from inferyard.platforms.identity import PreflightError, resolve_loopback_origin

BUILD = "b10743-adfffbe41"


class ProtocolError(RuntimeError):
    pass


class SSEDecoder:
    def __init__(self):
        self.decoder = codecs.getincrementaldecoder("utf-8")("strict")
        self.buffer = ""
        self.data = []

    def feed(self, raw: bytes) -> list[str]:
        self.buffer += self.decoder.decode(raw)
        if len(self.buffer) + sum(map(len, self.data)) > 8 * 1024**2:
            raise ProtocolError("sse_event_too_large")
        events = []
        while "\n" in self.buffer:
            line, self.buffer = self.buffer.split("\n", 1)
            line = line.removesuffix("\r")
            if not line:
                if self.data:
                    events.append("\n".join(self.data))
                    self.data.clear()
            elif line.startswith("data:"):
                value = line[5:]
                self.data.append(value[1:] if value.startswith(" ") else value)
        return events

    def finish(self):
        self.decoder.decode(b"", final=True)
        if self.buffer or self.data:
            raise ProtocolError("incomplete_sse_event")


class StreamProtocol:
    def __init__(
        self, state: ResponseState, redactor: Redactor, emit, *, arrivals=False, streaming=True
    ):
        self.state = state

        def emit_safely(*args):
            try:
                emit(*args)
            except EvidenceError:
                raise
            except Exception as exc:
                raise EvidenceError("event_sink_failed") from exc

        self.emit = emit_safely
        self.streams = {"content": redactor.stream(), "reasoning": redactor.stream()}
        self.arrivals = arrivals
        self.arrival_index = 0
        if arrivals:
            self.emit(
                "arrival_capture",
                {"streaming": streaming, "source": "decoded_delta"},
                state.t_send_ns,
            )

    def _text(self, channel, text, timestamp):
        if not isinstance(text, str):
            raise ProtocolError("invalid_content_type")
        if text:
            if self.arrivals:
                self.arrival_index += 1
                self.emit(
                    "block_arrived", {"index": self.arrival_index, "channel": channel}, timestamp
                )
            if self.state.t_first_content_ns is None:
                self.state.t_first_content_ns = timestamp
            if channel == "content" and self.state.t_first_answer_ns is None:
                self.state.t_first_answer_ns = timestamp
        safe = self.streams[channel].feed(text)
        getattr(self.state, channel).append(safe)
        self.emit(channel, {"text": safe}, timestamp)

    def flush(self, timestamp):
        for channel, redactor in self.streams.items():
            safe = redactor.feed("", final=True)
            if safe:
                getattr(self.state, channel).append(safe)
                self.emit(channel, {"text": safe}, timestamp)
            self.state.redacted |= redactor.changed

    def accept(self, data: str, timestamp: int):
        if self.state.done:
            raise ProtocolError("event_after_done")
        if data == "[DONE]":
            if self.state.finish_reason not in ("stop", "length"):
                raise ProtocolError("missing_or_unexpected_finish_reason")
            self.flush(timestamp)
            self.state.done = True
            self.emit("protocol_end", {}, timestamp)
            return
        try:
            event = strict_json_loads(data)
        except ContractError as exc:
            raise ProtocolError("invalid_sse_json") from exc
        if not isinstance(event, dict) or event.get("error"):
            raise ProtocolError("service_error_event")
        if self.arrivals and "timings" in event:
            choices = event.get("choices", [])
            final = self.state.finish_reason is not None or (
                isinstance(choices, list)
                and any(
                    isinstance(c, dict) and c.get("finish_reason") in ("stop", "length")
                    for c in choices
                )
            )
            self.emit("engine_timings", capture_timings(event["timings"], final=final), timestamp)
        usage = event.get("usage")
        if usage is not None:
            if not isinstance(usage, dict):
                raise ProtocolError("invalid_usage")
            for key in ("completion_tokens", "prompt_tokens"):
                value = usage.get(key)
                if value is not None and (type(value) is not int or value < 0):
                    raise ProtocolError("invalid_usage")
                setattr(self.state, key, value)
            self.emit(
                "usage",
                {
                    "completion_tokens": self.state.completion_tokens,
                    "prompt_tokens": self.state.prompt_tokens,
                    "source": "endpoint.usage",
                    "scope": "completion_tokens",
                },
                timestamp,
            )
        choices = event.get("choices")
        if not isinstance(choices, list) or len(choices) > 1:
            raise ProtocolError("invalid_choices")
        if not choices:
            return
        choice = choices[0]
        if (
            not isinstance(choice, dict)
            or type(choice.get("index")) is not int
            or choice["index"] != 0
        ):
            raise ProtocolError("invalid_choice")
        delta = choice.get("delta", {})
        if not isinstance(delta, dict):
            raise ProtocolError("invalid_delta")
        for field_name, channel in (
            ("content", "content"),
            ("reasoning_content", "reasoning"),
            ("reasoning", "reasoning"),
        ):
            if delta.get(field_name) is not None:
                if self.state.finish_reason is not None and delta[field_name] != "":
                    raise ProtocolError("content_after_finish")
                self._text(channel, delta[field_name], timestamp)
        reason = choice.get("finish_reason")
        if reason is not None:
            if not isinstance(reason, str) or self.state.finish_reason is not None:
                raise ProtocolError("invalid_finish_reason")
            self.state.finish_reason = reason
            self.emit("finish", {"raw_finish_reason": reason}, timestamp)
            if reason not in ("stop", "length"):
                raise ProtocolError("unexpected_finish_reason")


def request_body(config: dict, prompt: str, *, stream=True) -> dict:
    generation = dict(config["generation"])
    generation.pop("seed_support")
    reasoning = generation.pop("reasoning_mode")
    return {
        "model": config["model"]["display_name"],
        "messages": [{"role": "user", "content": prompt}],
        **generation,
        "n": 1,
        "stream": stream,
        "cache_prompt": config["conditions"]["cache_policy"] == "enabled",
        "chat_template_kwargs": {"enable_thinking": reasoning == "on"},
        **({"stream_options": {"include_usage": True}} if stream else {}),
    }


class PrismAdapter:
    build_info = BUILD
    idle_source = "/slots:is_processing"
    request_body = staticmethod(request_body)

    def __init__(self, origin: str, *, secret: str | None = None, transport=None):
        # Re-resolve and pin for real requests; MockTransport still uses a validated numeric URL.
        self.origin, _, _ = resolve_loopback_origin(origin)
        self.redactor = Redactor([secret] if secret else [])
        self.client = httpx.AsyncClient(
            base_url=self.origin,
            trust_env=False,
            follow_redirects=False,
            timeout=None,
            transport=transport,
            headers={"Authorization": "Bearer " + secret} if secret else {},
        )
        self.last_state = None
        self.capture_arrivals = False

    async def close(self):
        await self.client.aclose()

    async def management(self, path: str, body=None, *, timeout=5):
        try:
            async with asyncio.timeout(timeout):
                response = await self.client.request(
                    "GET" if body is None else "POST", path, json=body if body is not None else None
                )
                if response.status_code != 200:
                    raise PreflightError("management_http_error")
                return strict_json_loads(response.text)
        except (httpx.HTTPError, TimeoutError, ContractError) as exc:
            raise PreflightError("management_unavailable") from exc

    async def slots(self):
        slots = await self.management("/slots")
        if (
            not isinstance(slots, list)
            or len(slots) != 1
            or not isinstance(slots[0], dict)
            or type(slots[0].get("is_processing")) is not bool
            or slots[0].get("id") != 0
        ):
            raise PreflightError("unverified_slot_schema")
        return slots

    async def wait_idle(self, seconds=5, observe=None):
        deadline = asyncio.get_running_loop().time() + seconds
        while True:
            remaining = deadline - asyncio.get_running_loop().time()
            if remaining <= 0:
                return False
            try:
                async with asyncio.timeout(remaining):
                    slots = await self.slots()
                idle = slots[0]["is_processing"] is False
                if observe:
                    observe("idle" if idle else "busy", slots)
                if idle:
                    return True
            except PreflightError, TimeoutError:
                if observe:
                    observe("unknown", None)
            await asyncio.sleep(min(0.25, max(0, deadline - asyncio.get_running_loop().time())))

    async def token_budget(self, config: dict, prompt: str) -> dict:
        from inferyard.runtime.template_tokens import count_template

        record = await count_template(self, config, prompt)
        if (
            record["input_tokens"] + config["generation"]["max_tokens"]
            > config["conditions"]["context_size"]
        ):
            raise PreflightError("context_budget_exceeded")
        return record

    async def verify_properties(self, config: dict) -> dict:
        props = await self.management("/props")
        if not isinstance(props, dict) or props.get("build_info") != self.build_info:
            raise PreflightError("unsupported_engine_build")
        if (
            props.get("total_slots") != getattr(self, "expected_slots", 1)
            or props.get("default_generation_settings", {}).get("n_ctx")
            != config["conditions"]["context_size"]
            or props.get("model_path") != config["model"]["local_path"]
        ):
            raise PreflightError("service_properties_mismatch")
        template = props.get("chat_template")
        if (
            not isinstance(template, str)
            or hashlib.sha256(template.encode()).hexdigest() != config["model"]["template_sha256"]
        ):
            raise PreflightError("service_template_mismatch")
        args = config["engine"]["startup_args"]

        def option(*names):
            for i, argument in enumerate(args):
                if argument in names and i + 1 < len(args):
                    return args[i + 1]
            return None

        gpu_layers = option("-ngl", "--gpu-layers", "--n-gpu-layers")
        backend = config["engine"]["backend"]
        if backend in ("cuda", "metal"):
            try:
                if int(gpu_layers) <= 0:
                    raise ValueError
            except (TypeError, ValueError) as exc:
                raise PreflightError(f"{backend}_offload_not_requested") from exc
        elif gpu_layers != "0":
            raise PreflightError("required_startup_option_unverified")
        for actual, expected in (
            (option("-t", "--threads"), str(config["conditions"]["threads"])),
            (option("-tb", "--threads-batch"), str(config["conditions"]["threads_batch"])),
            (option("--reasoning"), config["generation"]["reasoning_mode"]),
        ):
            if actual != expected:
                raise PreflightError("required_startup_option_unverified")
        cache_allowed = config["conditions"]["cache_policy"] == "disabled" or (
            config["conditions"]["cache_policy"] == "enabled"
            and getattr(self, "cache_protocol", None) == "prism_prefix_reuse.v1"
        )
        if not cache_allowed or not disabled_startup(args):
            raise PreflightError("cache_policy_unverified")
        return props

    async def verify_effective(
        self, config: dict, expected_input_tokens: int, state: ResponseState
    ) -> dict:
        slots = await self.slots()
        params = slots[0].get("params", {})
        effective = {}
        for key in (
            "seed",
            "temperature",
            "top_k",
            "top_p",
            "min_p",
            "presence_penalty",
            "repeat_penalty",
            "max_tokens",
        ):
            requested = config["generation"][key]
            observed = params.get(key)
            if type(observed) not in (int, float) or not math.isclose(
                requested, observed, rel_tol=1e-6, abs_tol=1e-6
            ):
                raise PreflightError("generation_parameter_unverified")
            effective[key] = {
                "requested": requested,
                "effective": observed,
                "verification": "verified",
                "source": "slots.params",
            }
        if state.prompt_tokens != expected_input_tokens:
            raise PreflightError("template_token_count_mismatch")
        if config["generation"]["seed_support"] != "supported":
            raise PreflightError("seed_support_not_frozen")
        return {
            "parameters": effective,
            "slots": slots,
            "template_count_matches_usage": True,
            "stop": {
                "value": config["generation"]["stop"],
                "source": self.build_info + ":request_mapping",
            },
            "reasoning": {
                "value": config["generation"]["reasoning_mode"],
                "source": "startup+template",
            },
            "cache": (
                {
                    "value": config["conditions"]["cache_policy"],
                    "source": "frozen_prefix_reuse_request_policy_not_hit_evidence",
                }
                if getattr(self, "cache_protocol", None) == "prism_prefix_reuse.v1"
                else {"value": "disabled", "source": "startup+cache_prompt=false"}
            ),
        }

    async def generate(
        self, body: dict, timeout: float, emit=lambda *args: None, *, before_send=None
    ) -> dict | None:
        self.last_state = None
        send_ns = time.monotonic_ns()
        if before_send is not None and not before_send(send_ns):
            return None
        state = self.last_state = ResponseState(send_ns)
        protocol = StreamProtocol(
            state, self.redactor, emit, arrivals=self.capture_arrivals, streaming=body["stream"]
        )
        error = None
        terminal_time = None
        try:
            async with asyncio.timeout(timeout):
                async with self.client.stream(
                    "POST", "/v1/chat/completions", json=body
                ) as response:
                    if self.capture_arrivals:
                        emit(
                            "http_response",
                            {"status_code": response.status_code},
                            time.monotonic_ns(),
                        )
                    if response.status_code != 200:
                        raise ProtocolError("http_error")
                    if body["stream"]:
                        decoder = SSEDecoder()
                        async for raw in response.aiter_bytes():
                            now = time.monotonic_ns()
                            for event in decoder.feed(raw):
                                protocol.accept(event, now)
                                if state.done:
                                    terminal_time = now
                            if state.done:
                                break
                        if not state.done:
                            decoder.finish()
                            raise ProtocolError("missing_protocol_end")
                    else:
                        event = strict_json_loads((await response.aread()).decode("utf-8"))
                        choices = event.get("choices") if isinstance(event, dict) else None
                        if not isinstance(choices, list) or len(choices) != 1:
                            raise ProtocolError("invalid_ordinary_response")
                        choice = choices[0]
                        if not isinstance(choice, dict):
                            raise ProtocolError("invalid_ordinary_choice")
                        message = choice.get("message")
                        if not isinstance(message, dict) or not isinstance(
                            message.get("content"), str
                        ):
                            raise ProtocolError("invalid_ordinary_message")
                        now = time.monotonic_ns()
                        protocol.accept(
                            json.dumps(
                                {
                                    "choices": [
                                        {
                                            "index": 0,
                                            "delta": message,
                                            "finish_reason": choice.get("finish_reason"),
                                        }
                                    ],
                                    "usage": event.get("usage"),
                                    **({"timings": event["timings"]} if "timings" in event else {}),
                                }
                            ),
                            now,
                        )
                        protocol.accept("[DONE]", now)
                        terminal_time = now
        except TimeoutError:
            error = "total_timeout"
        except httpx.HTTPError:
            error = "transport_error"
        except ProtocolError as exc:
            # These are fixed adapter-owned categories, never service error text.
            error = str(exc)
        except ContractError, UnicodeError, ValueError, KeyError, TypeError:
            error = "protocol_error"
        except asyncio.CancelledError:
            protocol.flush(time.monotonic_ns())
            raise
        now = terminal_time if terminal_time is not None and error is None else time.monotonic_ns()
        protocol.flush(now)
        return state.terminal(now, error)
