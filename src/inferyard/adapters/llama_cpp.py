"""Pinned upstream b11146 CPU adapter; ordinary/SSE contracts share the parser.

Source: ggml-org/llama.cpp commit 7fe450e19305b828c199d602c23a8337aaa1f03b.
Binary/model/library identity is additionally checked by static_preflight.
The shared HTTP parser does not grant performance comparison qualification.
"""

from inferyard.adapters.prism import PrismAdapter
from inferyard.platforms.identity import PreflightError

BUILD = "b11146-7fe450e19"
COMPONENT = "llama_cpp_b11146_v1"


class LlamaCppAdapter(PrismAdapter):
    build_info = BUILD

    async def verify_properties(self, config):
        if config["engine"]["adapter"] != COMPONENT or config["engine"]["release"] != BUILD:
            raise PreflightError("unsupported_engine_release")
        if config["engine"]["backend"] != "cpu":
            raise PreflightError("unsupported_engine_backend")
        if config["engine"].get("slots_debug") is not True:
            raise PreflightError("upstream_slots_debug_not_frozen")
        if getattr(self, "cache_protocol", None) is not None:
            raise PreflightError("upstream_cache_protocol_not_implemented")
        props = await super().verify_properties(config)
        if props.get("endpoint_slots") is not True:
            raise PreflightError("upstream_slots_endpoint_unverified")
        return props

    async def verify_effective(self, config, expected_input_tokens, state):
        result = await super().verify_effective(config, expected_input_tokens, state)
        params = result["slots"][0].get("params", {})
        # b11146 keeps both spellings; reject inconsistent or missing evidence.
        if params.get("n_predict") != config["generation"]["max_tokens"]:
            raise PreflightError("generation_parameter_unverified")
        if params.get("stop") != config["generation"]["stop"]:
            raise PreflightError("stop_parameter_unverified")
        result["stop"] = {
            "value": config["generation"]["stop"],
            "source": "slots.params.stop",
        }
        return result
