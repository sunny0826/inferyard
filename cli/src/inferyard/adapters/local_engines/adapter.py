"""Local engines retain native/client_http completion, never inferred GPU drain."""

import asyncio
import json
import time
from copy import deepcopy

from inferyard.adapters.engine_fit import FitTransportError, _json
from inferyard.adapters.lab_adapter import LabAdapter
from inferyard.adapters.local_engines.protocol import VERSION_PATHS, models, queue, service
from inferyard.platforms.engine_fit_lmstudio import LMStudioObserver
from inferyard.platforms.identity import PreflightError
from inferyard.platforms.local_engines import verify_bound_service


class LocalEngineAdapter(LabAdapter):
    engine_id = "local_openai_v1"
    local_profile = True
    observation_mode = "native"

    def __init__(
        self, origin, *, secret=None, transport=None, binding_verifier=None, observer=None
    ):
        super().__init__(origin, secret=secret, transport=transport)
        self._verify_binding = binding_verifier or verify_bound_service
        self._observer = observer
        self._service_binding = None
        self._config = None
        self.queue_ready = False

    @staticmethod
    def request_body(config, prompt, *, stream=True):
        return {
            "model": config["engine"]["served_model"],
            "messages": [{"role": "user", "content": prompt}],
            "temperature": config["generation"]["temperature"],
            "max_tokens": config["generation"]["max_tokens"],
            "stream": stream,
            **({"stream_options": {"include_usage": True}} if stream else {}),
        }

    async def _bound(self, deadline):
        async with asyncio.timeout_at(deadline):
            value = await asyncio.to_thread(self._verify_binding, self._config)
        if self._service_binding is not None and value != self._service_binding:
            raise PreflightError("local_engine_bound_service_changed")
        self._service_binding = deepcopy(value)
        return value

    async def _endpoint(self, path, deadline, matrix):
        record = await self.inspect_endpoint(path, deadline=deadline)
        matrix[path] = record
        if record["available"] is not True:
            raise PreflightError("local_engine_observation_unavailable")
        return record["body"]

    async def discover(self, config):
        self._config = deepcopy(config)
        family = config["engine"]["family"]
        deadline = time.monotonic() + config["execution"]["idle_wait_seconds"]
        matrix = {}
        try:
            binding = await self._bound(deadline)
            if family == "lmstudio" and self._observer is None:
                self._observer = LMStudioObserver(binding)
            models(
                await self._endpoint("/v1/models", deadline, matrix),
                config["engine"]["served_model"],
            )
            if family == "lmstudio":
                async with asyncio.timeout_at(deadline):
                    payload = await self._observer.inspect()
            else:
                payload = _json(await self._endpoint(VERSION_PATHS[family], deadline, matrix))
            properties = service(family, payload, config)
            if family != "lmstudio":
                await self._endpoint("/metrics", deadline, matrix)
            else:
                async with asyncio.timeout_at(deadline):
                    normalized = queue(
                        family, config["engine"]["served_model"], None, await self._observer.idle()
                    )
                matrix["lms:ps"] = {
                    "available": True,
                    "missing_reason": None,
                    "status_code": None,
                    "body": json.dumps(normalized, sort_keys=True),
                }
            await self._bound(deadline)
        except (FitTransportError, TimeoutError) as exc:
            raise PreflightError("local_engine_discovery_failed:" + str(exc)) from exc
        self.capability_evidence = {
            "definition": "local-engine-observation.v1",
            "mode": "native",
            "engine": self.engine_id,
            "family": family,
            "generation": "/v1/chat/completions",
            "endpoints": matrix,
            "request_id_scope": "client_only",
            "engine_internal_drain": {
                "value": None,
                "scope": "capability_only",
                "missing_reason": "native_lifecycle_unavailable",
            },
            "native_cancel": {
                "value": None,
                "missing_reason": "request_cancel_endpoint_not_discovered",
            },
            "process_evidence": "bound_local_process_and_assets_not_engine_drain",
        }
        self._properties = properties
        return self.capability_evidence

    async def verify_properties(self, config):
        if config["engine"]["adapter"] != self.engine_id:
            raise PreflightError("local_engine_adapter_mismatch")
        if self._config is not None and config != self._config:
            raise PreflightError("local_engine_configuration_changed")
        self.drain_seconds = config["execution"]["idle_wait_seconds"]
        if self.capability_evidence is None:
            await self.discover(config)
        else:
            await self._bound(time.monotonic() + config["execution"]["idle_wait_seconds"])
        return {"service": self._properties, "observation": self.capability_evidence}

    async def token_budget(self, config, prompt):
        return self.native_budget(config)

    async def verify_effective(self, config, expected_input_tokens, state):
        return self.native_parameters(config)

    async def generate(self, body, timeout, emit=lambda *args: None, *, before_send=None):
        if not self.queue_ready or self.native_uncertain:
            raise PreflightError("local_engine_queue_admission_required")
        self.queue_ready = False
        try:
            result = await super().generate(body, timeout, emit, before_send=before_send)
        except BaseException:
            self.native_uncertain = True
            raise
        if result is not None and result["execution_state"] != "completed":
            self.native_uncertain = True
        return result

    async def wait_ready(self, seconds=5, observe=None):
        self.queue_ready = False
        if self._config is None:
            raise PreflightError("local_engine_discovery_required")
        family = self._config["engine"]["family"]
        deadline = time.monotonic() + seconds
        while True:
            signals = {}
            snapshot = {
                "definition": "local-engine-queue.v1",
                "family": family,
                "served_model": self._config["engine"]["served_model"],
                "binding": deepcopy(self._service_binding),
                "source": "lms:ps" if family == "lmstudio" else "/metrics",
                "raw_metrics": None,
                "observation": None,
                "client_uncertain": self.native_uncertain,
                "missing_reason": None,
            }
            try:
                await self._bound(deadline)
                if family == "lmstudio":
                    async with asyncio.timeout_at(deadline):
                        observed = await self._observer.idle()
                else:
                    snapshot["raw_metrics"] = await self._endpoint("/metrics", deadline, signals)
                    observed = None
                snapshot["observation"] = queue(
                    family, snapshot["served_model"], snapshot["raw_metrics"], observed
                )
                await self._bound(deadline)
            except asyncio.CancelledError:
                self.native_uncertain = True
                raise
            except PreflightError, FitTransportError, TimeoutError:
                snapshot["observation"] = None
                snapshot["missing_reason"] = "local_engine_queue_unverified"
                self.native_uncertain = True
            snapshot["client_uncertain"] = self.native_uncertain
            signals["queue_snapshot"] = snapshot
            self.queue_ready = (
                snapshot["observation"] is not None
                and snapshot["observation"]["idle"]
                and not self.native_uncertain
            )
            if observe:
                observe(
                    "unknown",
                    {
                        "signals": signals,
                        "scope": "native_endpoints",
                        "engine_internal_drain": None,
                        "missing_reason": "native_lifecycle_unavailable",
                        "cancellation": getattr(self, "native_cancellation", None),
                    },
                )
            if self.queue_ready:
                return True
            if self.native_uncertain or time.monotonic() >= deadline:
                return False
            await asyncio.sleep(min(0.02, max(0, deadline - time.monotonic())))
