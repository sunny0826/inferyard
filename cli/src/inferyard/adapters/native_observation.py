"""Bounded, non-mutating capability discovery for unpatched release binaries."""

import asyncio
import time

import httpx

from inferyard.adapters.lab_generation import GenerationDecoder
from inferyard.adapters.lab_observation import ObservationTracker, parse_identity
from inferyard.adapters.lab_observation_json import PROTOCOL, LabProtocolError, load_strict
from inferyard.platforms.identity import PreflightError

PATHS = (
    "/lab/v1/identity",
    "/lab/v1/lifecycle",
    "/health",
    "/v1/models",
    "/slots",
    "/metrics",
    "/props",
)


class NativeObservation:
    observation_mode = None

    async def discover(self, config):
        matrix, raw_identity = {}, None
        required = config["engine"].get("observation_mode", "auto") == "lab_required"
        for path in PATHS:
            if path == "/lab/v1/lifecycle" and self.binding is None:
                # Discovery still records this endpoint; it cannot bind lifecycle evidence.
                if required:
                    raise PreflightError(
                        "lab_contract_http_error: required_lab_observation_unavailable"
                    )
            record = await self.inspect_endpoint(path, deadline=time.monotonic() + 5)
            if path == "/lab/v1/identity" and record["available"] is True:
                raw_identity = record["body"]
                try:
                    claimed = load_strict(raw_identity.encode(), "lab_invalid_identity")
                    if (
                        isinstance(claimed, dict)
                        and claimed.get("protocol") == PROTOCOL
                        and claimed.get("engine") != self.engine_id
                    ):
                        raise PreflightError("lab_engine_mismatch")
                    self.binding = parse_identity(
                        raw_identity.encode(), expected_engine=self.engine_id
                    )
                    self.tracker = ObservationTracker(self.binding["server_instance_id"])
                except LabProtocolError:
                    pass
            matrix[path] = record
        if raw_identity is not None:
            try:
                value = parse_identity(raw_identity.encode(), expected_engine=self.engine_id)
                self.binding = value
                self.tracker = ObservationTracker(value["server_instance_id"])
                self.observation_mode = (
                    "lab"
                    if all(value["capabilities"].values())
                    and matrix["/lab/v1/lifecycle"]["available"] is True
                    else "native"
                )
            except LabProtocolError as exc:
                if required:
                    raise PreflightError(str(exc)) from exc
                matrix["/lab/v1/identity"].update(available=None, missing_reason=str(exc))
        self.observation_mode = self.observation_mode or "native"
        if required and (
            self.observation_mode != "lab" or not all(self.binding["capabilities"].values())
        ):
            raise PreflightError("lab_required_capability_missing")
        self.capability_evidence = {
            "mode": self.observation_mode,
            "engine": self.engine_id,
            "generation": "/v1/chat/completions",
            "endpoints": matrix,
            "request_id_scope": "engine" if self.observation_mode == "lab" else "client_only",
            "engine_internal_drain": {
                "value": True if self.observation_mode == "lab" else None,
                "scope": "capability_only",
                "missing_reason": None
                if self.observation_mode == "lab"
                else "native_lifecycle_unavailable",
            },
            "native_cancel": {
                "value": None,
                "missing_reason": "request_cancel_endpoint_not_discovered",
            },
            "process_evidence": "Windows PID/FILETIME/exe/argv/cwd/assets; not engine drain proof",
        }
        return self.capability_evidence

    async def inspect_endpoint(self, path, *, deadline):
        status, body = None, None
        reason = None
        try:
            async with asyncio.timeout_at(deadline):
                headers = self.headers() if self.binding is not None else {}
                async with self.client.stream("GET", path, headers=headers) as result:
                    status = result.status_code
                    raw = bytearray()
                    async for chunk in result.aiter_bytes():
                        raw.extend(chunk)
                        if len(raw) > 2 * 1024**2:
                            raise PreflightError("native_observation_size")
                    if time.monotonic() >= deadline:
                        raise TimeoutError
                    if status == 200:
                        body = self.redactor.clean(bytes(raw).decode("utf-8"))
                    else:
                        reason = f"http_{status}"
        except TimeoutError, httpx.HTTPError, UnicodeError, PreflightError:
            reason = "native_observation_unavailable"
        return {
            "available": True if body is not None else None,
            "missing_reason": reason,
            "status_code": status,
            "body": body,
        }

    async def wait_ready(self, seconds=5, observe=None):
        # Serial admission is a client state, never an engine-wide idle assertion.
        snapshots = {}
        deadline = time.monotonic() + seconds
        for path in ("/health", "/v1/models", "/slots", "/metrics"):
            if self.capability_evidence["endpoints"][path]["available"] is True:
                snapshots[path] = await self.inspect_endpoint(path, deadline=deadline)
        if observe:
            observe(
                "unknown",
                {
                    "signals": snapshots,
                    "scope": "native_endpoints",
                    "engine_internal_drain": None,
                    "missing_reason": "native_lifecycle_unavailable",
                    "cancellation": getattr(self, "native_cancellation", None),
                },
            )
        return not self.native_uncertain

    def native_budget(self, config):
        return {
            "input_tokens": None,
            "output_budget": config["generation"]["max_tokens"],
            "template_prompt_sha256": None,
            "source": None,
            "verification": "not_observed",
            "missing_reason": "exact_template_budget_unavailable",
        }

    def native_parameters(self, config):
        return {
            "parameters": {
                key: {
                    "requested": value,
                    "effective": None,
                    "source": None,
                    "verification": "not_observed",
                    "missing_reason": "effective_parameter_unavailable",
                }
                for key, value in config["generation"].items()
            },
            "template_count_matches_usage": None,
            "missing_reason": "exact_template_budget_unavailable",
        }


class OpenAIGenerationDecoder(GenerationDecoder):
    def _check_ids(self, payload):
        # OpenAI's response id is server-generated; it is not a lab/client binding.
        pass

    def _accept_message(self, message, now):
        message = dict(message)
        if "reasoning_content" in message and "reasoning" not in message:
            message["reasoning"] = message["reasoning_content"]
        return super()._accept_message(message, now)

    def _accept_chunk_choice(self, choice, now):
        choice = dict(choice)
        delta = choice.get("delta")
        if isinstance(delta, dict) and "reasoning_content" in delta and "reasoning" not in delta:
            choice["delta"] = {**delta, "reasoning": delta["reasoning_content"]}
        return super()._accept_chunk_choice(choice, now)
