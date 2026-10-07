"""Formal serial text adapter with discovered lab or native observation capabilities."""

import asyncio
import codecs
import time
import uuid

import httpx

from inferyard.adapters.lab_generation import GenerationDecoder
from inferyard.adapters.lab_observation import is_idle, parse_cancel
from inferyard.adapters.lab_observation_json import LabProtocolError
from inferyard.adapters.lab_parameters import LabParameters
from inferyard.adapters.lab_transport import LabTransport
from inferyard.adapters.native_observation import NativeObservation, OpenAIGenerationDecoder
from inferyard.adapters.response_state import ResponseState
from inferyard.evidence.storage import EvidenceError, Redactor
from inferyard.platforms.identity import PreflightError, resolve_loopback_origin


class LabAdapter(LabParameters, NativeObservation, LabTransport):
    halt_on_failed = True
    idle_source = "/lab/v1/lifecycle"
    engine_id = None

    def __init__(self, origin, *, secret=None, transport=None):
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
        self.binding = self.tracker = self.last_state = None
        self.pending_request = None
        self.capture_arrivals = False
        self.drain_seconds = 5
        self.observations = []
        self.native_uncertain = False
        self.capability_evidence = None
        self.next_request_id = None

    async def close(self):
        await self.client.aclose()

    @staticmethod
    def request_body(config, prompt, *, stream=True):
        generation = dict(config["generation"])
        generation.pop("seed_support")
        generation.pop("reasoning_mode")
        return {
            "model": config["model"]["display_name"],
            "messages": [{"role": "user", "content": prompt}],
            **generation,
            "n": 1,
            "stream": stream,
            **({"stream_options": {"include_usage": True}} if stream else {}),
        }

    def prepare_request(self, body):
        if self.observation_mode == "native":
            self.next_request_id = uuid.uuid4().hex
            return dict(body)
        if self.binding is None:
            raise PreflightError("lab_identity_not_bound")
        return {
            **body,
            "lab_request_id": uuid.uuid4().hex,
            "lab_server_instance_id": self.binding["server_instance_id"],
        }

    async def wait_idle(self, seconds=5, observe=None):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            await self.identity(deadline=deadline)
            request = None
            if self.pending_request is not None:
                request = await self.request_status(self.pending_request, deadline=deadline)
            snapshot = await self.lifecycle(deadline=deadline)
            # Read identity again: an instance change during the observation is a refusal.
            await self.identity(deadline=deadline)
            idle = is_idle(snapshot) and (request is None or request["phase"] == "released")
            evidence = {
                "lifecycle": snapshot,
                "request": request,
                "cancellation": self.observations[-1] if self.observations else None,
            }
            if observe:
                observe("idle" if idle else "busy", evidence)
            if idle:
                self.pending_request = None
                self.observations.clear()
                return True
            await asyncio.sleep(min(0.02, max(0, deadline - time.monotonic())))
        return False

    async def cancel(self, request_id):
        if self.observation_mode == "native":
            self.native_uncertain = True
            self.native_cancellation = {
                "client_action": "close_http_response_context",
                "native_cancel": None,
                "missing_reason": "request_cancel_endpoint_not_discovered",
            }
            return self.native_cancellation
        raw = await self.read(
            f"/lab/v1/requests/{request_id}/cancel",
            {},
            deadline=time.monotonic() + self.drain_seconds,
            headers=self.headers(),
        )
        try:
            value = parse_cancel(
                raw,
                expected_instance_id=self.binding["server_instance_id"],
                expected_request_id=request_id,
            )
        except LabProtocolError as exc:
            raise PreflightError(str(exc)) from exc
        if "error" in value:
            raise PreflightError("lab_cancel_unconfirmed")
        self.observations.append(value)
        return value

    async def generate(self, body, timeout, emit=lambda *args: None, *, before_send=None):
        self.last_state = None
        native = self.observation_mode == "native"
        if self.binding is None and not native:
            raise PreflightError("lab_identity_not_bound")
        if not native and "lab_request_id" not in body:
            body = self.prepare_request(body)
        request_id = (
            (self.next_request_id or uuid.uuid4().hex) if native else body["lab_request_id"]
        )
        self.next_request_id = None
        instance_id = "0" * 32 if native else self.binding["server_instance_id"]
        if not native and body.get("lab_server_instance_id") != instance_id:
            raise PreflightError("lab_instance_mismatch")
        send_ns = time.monotonic_ns()
        decoder = (OpenAIGenerationDecoder if native else GenerationDecoder)(
            request_id, instance_id, streaming=body["stream"], t_send_ns=send_ns
        )
        if before_send is not None and not before_send(send_ns):
            return None
        state = self.last_state = ResponseState(send_ns)
        state.request_id = request_id
        self.pending_request = request_id
        if native:
            self.native_uncertain = True
        text_streams = {key: self.redactor.stream() for key in ("content", "reasoning")}
        lengths = dict.fromkeys(text_streams, 0)
        wire_utf8 = codecs.getincrementaldecoder("utf-8")("strict")
        wire_redactor = self.redactor.stream()
        arrival_index = 0
        wire_bytes = 0
        previous_usage = None

        def safe_emit(kind, data, now):
            try:
                emit(kind, data, now)
            except EvidenceError:
                raise
            except Exception as exc:
                raise EvidenceError("event_sink_failed") from exc

        def sync(now, *, final=False):
            nonlocal arrival_index, previous_usage
            value = decoder.snapshot()
            state.prompt_tokens = value["prompt_tokens"]
            state.completion_tokens = value["completion_tokens"]
            state.t_first_content_ns = value["t_first_content_ns"]
            state.t_first_answer_ns = value["t_first_answer_ns"]
            for key, stream in text_streams.items():
                delta = value[key][lengths[key] :]
                lengths[key] = len(value[key])
                if delta.strip() and self.capture_arrivals:
                    arrival_index += 1
                    safe_emit("block_arrived", {"index": arrival_index, "channel": key}, now)
                safe = stream.feed(delta, final=final)
                if safe:
                    getattr(state, key).append(safe)
                    safe_emit(key, {"text": safe}, now)
                state.redacted |= stream.changed
            if state.finish_reason is None and value["finish_reason"] is not None:
                state.finish_reason = value["finish_reason"]
                safe_emit("finish", {"raw_finish_reason": state.finish_reason}, now)
            usage = (state.prompt_tokens, state.completion_tokens)
            if usage != previous_usage and any(v is not None for v in usage):
                safe_emit(
                    "usage",
                    {
                        "prompt_tokens": usage[0],
                        "completion_tokens": usage[1],
                        "source": "endpoint.usage",
                        "scope": "completion_tokens",
                    },
                    now,
                )
                previous_usage = usage

        def wire(raw, now, *, final=False):
            text = wire_redactor.feed(wire_utf8.decode(raw, final=final), final=final)
            if text:
                safe_emit(
                    "native_wire" if native else "lab_wire",
                    {"client_request_id": request_id, "text": text}
                    if native
                    else {
                        "request_id": request_id,
                        "server_instance_id": instance_id,
                        "text": text,
                    },
                    now,
                )
            state.redacted |= wire_redactor.changed

        error = None
        terminal_ns = None
        try:
            if self.capture_arrivals:
                safe_emit(
                    "arrival_capture",
                    {"streaming": body["stream"], "source": "decoded_delta"},
                    send_ns,
                )
            deadline = time.monotonic() + timeout
            async with asyncio.timeout_at(deadline):
                async with self.client.stream(
                    "POST", "/v1/chat/completions", json=body, headers=self.headers(request_id)
                ) as response:
                    safe_emit(
                        "http_response", {"status_code": response.status_code}, time.monotonic_ns()
                    )
                    if response.status_code != 200:
                        raise LabProtocolError("http_error")
                    async for raw in response.aiter_bytes():
                        now = time.monotonic_ns()
                        wire_bytes += len(raw)
                        if wire_bytes > 2 * 1024**2:
                            raise LabProtocolError("lab_generation_size")
                        wire(raw, now)
                        try:
                            decoder.feed(raw, observed_ns=now)
                        finally:
                            sync(now)
                        if time.monotonic() >= deadline:
                            raise TimeoutError
                    now = time.monotonic_ns()
                    try:
                        value = decoder.finish(observed_ns=now)
                    finally:
                        sync(now)
                    wire(b"", now, final=True)
                    sync(now, final=True)
                    if time.monotonic() >= deadline:
                        raise TimeoutError
                    state.done = True
                    if native:
                        self.native_uncertain = False
                        self.pending_request = None
                    terminal_ns = now
                    safe_emit(
                        "usage",
                        {
                            "prompt_tokens": state.prompt_tokens,
                            "completion_tokens": state.completion_tokens,
                            "source": "endpoint.usage",
                            "scope": "completion_tokens",
                        },
                        now,
                    )
                    safe_emit(
                        "lab_usage",
                        {
                            key: value[key]
                            for key in (
                                "prompt_tokens",
                                "completion_tokens",
                                "cached_tokens",
                                "reasoning_tokens",
                                "usage_missing_reasons",
                            )
                        },
                        now,
                    )
                    safe_emit("protocol_end", {}, now)
        except TimeoutError:
            error = "total_timeout"
        except httpx.HTTPError:
            error = "transport_error"
        except LabProtocolError as exc:
            error = str(exc)
        except UnicodeError:
            error = "lab_generation_utf8"
        except BaseException:
            try:
                sync(time.monotonic_ns(), final=True)
            finally:
                try:
                    await self.cancel(request_id)
                except PreflightError:
                    pass  # Pending ID remains; wait_idle must independently prove release.
            raise
        if error is not None:
            sync(time.monotonic_ns(), final=True)
            try:
                await self.cancel(request_id)
            except PreflightError:
                error += "_cancel_unconfirmed"
        now = terminal_ns if error is None else time.monotonic_ns()
        return state.terminal(now, error)

    def headers(self, request_id=None):
        return {} if self.observation_mode == "native" else super().headers(request_id)
