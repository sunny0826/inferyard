"""Pinned upstream transport for separately versioned concurrency/tool experiments.

Source: ggml-org/llama.cpp 7fe450e19305b828c199d602c23a8337aaa1f03b.
Any live use additionally requires process/library/model preflight and HostLock.
"""

import asyncio
import time

import httpx

from inferyard.adapters.llama_cpp import BUILD
from inferyard.adapters.prism import PrismAdapter, SSEDecoder, request_body
from inferyard.analysis.scoring import score_case
from inferyard.contracts.validation import strict_json_loads
from inferyard.evidence.storage import EvidenceError
from inferyard.extensions.native_tool_stream import ToolStream
from inferyard.extensions.native_tools import parse_reply
from inferyard.platforms.identity import PreflightError


class ExtensionTransport(PrismAdapter):
    build_info = BUILD

    def __init__(
        self,
        config,
        cases,
        *,
        answer_policy=None,
        secret=None,
        concurrency=1,
        emit=None,
        transport=None,
    ):
        super().__init__(config["endpoint"]["url"], secret=secret, transport=transport)
        self.config, self.cases = config, {c["case_id"]: c for c in cases}
        self.concurrency, self.record = concurrency, emit or (lambda *args: None)
        self.expected_slots = concurrency
        self.answer_policy = answer_policy or {"strip_line_edges": True, "ignore_empty_lines": True}

    async def slots(self):
        rows = await self.management("/slots")
        if (
            not isinstance(rows, list)
            or len(rows) != self.concurrency
            or any(
                not isinstance(r, dict)
                or type(r.get("id")) is not int
                or type(r.get("is_processing")) is not bool
                for r in rows
            )
            or sorted(r["id"] for r in rows) != list(range(self.concurrency))
        ):
            raise PreflightError("extension_slot_identity_unverified")
        return sorted(rows, key=lambda r: r["id"])

    async def verify(self, *, tools=False):
        config = self.config
        if (
            config["engine"]["adapter"] != "llama_cpp_b11146_v1"
            or config["engine"]["release"] != BUILD
            or not config["engine"].get("slots_debug")
        ):
            raise PreflightError("extension_requires_pinned_upstream_debug_slots")
        if config["conditions"]["cache_policy"] != "disabled":
            raise PreflightError("extension_cache_protocol_not_qualified")
        if any(
            arg.split("=", 1)[0] in ("--agent", "--tools", "--tools-runtime", "--mcp-server")
            for arg in config["engine"]["startup_args"]
        ):
            raise PreflightError("extension_server_side_tools_not_allowed")
        props = await super().verify_properties(config)
        if (
            props.get("build_info") != BUILD
            or props.get("total_slots") != self.concurrency
            or props.get("endpoint_slots") is not True
        ):
            raise PreflightError("extension_properties_unverified")
        if tools and props.get("chat_template_caps", {}).get("supports_tool_calls") is not True:
            raise PreflightError("extension_native_tools_capability_unverified")
        template = await self.management(
            "/apply-template", {"messages": [{"role": "user", "content": "模板核验"}]}
        )
        if not isinstance(template.get("prompt"), str):
            raise PreflightError("extension_template_unavailable")
        slots = await self.slots()
        if any(r["is_processing"] for r in slots):
            raise PreflightError("extension_slots_busy")
        self.record("capability", {"props": props, "slots": slots})
        return props

    async def wait_idle(self, slot, timeout):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if not (await self.slots())[slot]["is_processing"]:
                return True
            await asyncio.sleep(0.05)
        return False

    async def probe_cancellation(self):
        """One diagnostic only: observe busy, close this connection, then prove idle."""
        first = next(iter(self.cases.values()))
        body = request_body(self.config, first["prompt"], stream=True)
        body.update(id_slot=0)

        async def hold():
            async with self.client.stream("POST", "/v1/chat/completions", json=body) as response:
                if response.status_code != 200:
                    raise EvidenceError("extension_cancel_probe_http_error")
                await asyncio.sleep(3)

        task = asyncio.create_task(hold())
        busy = False
        try:
            async with asyncio.timeout(3):
                while not task.done():
                    if (await self.slots())[0]["is_processing"]:
                        busy = True
                        break
                    await asyncio.sleep(0.01)
        finally:
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)
        idle = await self.wait_idle(0, 5)
        self.record(
            "cancellation_probe", {"slot_id": 0, "busy_observed": busy, "idle_after_cancel": idle}
        )
        if not busy or not idle:
            raise PreflightError("extension_cancellation_capability_unverified")

    async def completion(self, messages, *, slot=0, tools=None, stream=False):
        body = request_body(self.config, "", stream=stream)
        body.update(messages=messages, id_slot=slot)
        if tools:
            body.update(
                tools=tools, tool_choice="auto", parallel_tool_calls=False, parse_tool_calls=True
            )
        try:
            async with asyncio.timeout(self.config["execution"]["timeout_seconds"]):
                expected_tool_tokens = await self.tool_input_tokens(body) if tools else None
                if not stream:
                    response = await self.client.post("/v1/chat/completions", json=body)
                    if response.status_code != 200 or len(response.content) > 1024 * 1024:
                        raise EvidenceError("extension_completion_http_or_size_error")
                    reply = strict_json_loads(response.text)
                    if tools:
                        await self.verify_tool_parameters(expected_tool_tokens, reply, slot)
                    return reply
                decoder, protocol = SSEDecoder(), ToolStream()
                async with self.client.stream(
                    "POST", "/v1/chat/completions", json=body
                ) as response:
                    if response.status_code != 200:
                        raise EvidenceError("extension_completion_http_error")
                    async for raw in response.aiter_bytes():
                        for data in decoder.feed(raw):
                            protocol.feed(data)
                    decoder.finish()
                reply = protocol.finish()
                if tools:
                    await self.verify_tool_parameters(expected_tool_tokens, reply, slot)
                return reply
        except httpx.HTTPError as exc:
            raise EvidenceError("extension_transport_failure") from exc

    async def infer(self, case_id, slot):
        case = self.cases[case_id]
        count = await self.token_budget(self.config, case["prompt"])
        reply = await self.completion([{"role": "user", "content": case["prompt"]}], slot=slot)
        message, reason = parse_reply(reply)
        if reason != "stop":
            raise EvidenceError("extension_answer_finish_invalid")
        usage = reply.get("usage", {})
        if usage.get("prompt_tokens") != count["input_tokens"]:
            raise PreflightError("extension_template_count_mismatch")
        # Effective numeric/stop parameters must be visible for this particular slot.
        params = await self.verify_slot_parameters(slot)
        answer = message["content"]
        redacted = self.redactor.text(answer)
        quality = (
            None
            if answer != redacted
            else score_case(case, answer, self.answer_policy)["quality_state"] == "pass"
        )
        self.record(
            "effective_parameters",
            {"case_id": case_id, "slot_id": slot, "params": params, "usage": usage},
        )
        return {
            "state": "completed",
            "protocol_complete": True,
            "quality_pass": quality,
            "answer": redacted,
            "usage": usage,
        }

    async def tool_input_tokens(self, body):
        template = await self.management(
            "/apply-template",
            {
                "messages": body["messages"],
                "tools": body["tools"],
                "add_generation_prompt": True,
                "chat_template_kwargs": body["chat_template_kwargs"],
            },
        )
        if not isinstance(template.get("prompt"), str):
            raise PreflightError("extension_tool_template_unverified")
        tokens = await self.management(
            "/tokenize", {"content": template["prompt"], "add_special": True, "parse_special": True}
        )
        ids = tokens.get("tokens")
        if not isinstance(ids, list) or any(type(t) is not int or t < 0 for t in ids):
            raise PreflightError("extension_tool_tokens_unverified")
        if (
            len(ids) + self.config["generation"]["max_tokens"]
            > self.config["conditions"]["context_size"]
        ):
            raise PreflightError("extension_tool_context_budget_exceeded")
        return len(ids)

    async def verify_tool_parameters(self, expected_tokens, reply, slot):
        if reply.get("usage", {}).get("prompt_tokens") != expected_tokens:
            raise PreflightError("extension_tool_template_count_mismatch")
        params = await self.verify_slot_parameters(slot)
        self.record(
            "tool_effective_parameters",
            {
                "slot_id": slot,
                "params": params,
                "usage": reply.get("usage"),
                "input_tokens": expected_tokens,
            },
        )

    async def verify_slot_parameters(self, slot):
        row = (await self.slots())[slot]
        params = row.get("params", {})
        for key in (
            "seed",
            "temperature",
            "top_k",
            "top_p",
            "min_p",
            "presence_penalty",
            "repeat_penalty",
        ):
            import math

            value = params.get(key)
            if type(value) not in (int, float) or not math.isclose(
                value, self.config["generation"][key], rel_tol=1e-6, abs_tol=1e-6
            ):
                raise PreflightError("extension_effective_parameter_unverified")
        if (
            params.get("n_predict") != self.config["generation"]["max_tokens"]
            or params.get("stop") != self.config["generation"]["stop"]
        ):
            raise PreflightError("extension_output_parameters_unverified")
        return params
