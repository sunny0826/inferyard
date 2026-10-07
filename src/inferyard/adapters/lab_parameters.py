"""Exact template budgets and effective settings for the formal lab CLI contract."""

import time

from inferyard.adapters.lab_observation_json import (
    PROTOCOL,
    LabProtocolError,
    load_strict,
    require_exact_keys,
    require_int,
    require_object,
    require_sha256,
)
from inferyard.contracts.schemas_config import CONFIG
from inferyard.contracts.validation import ContractError, _validate
from inferyard.platforms.identity import PreflightError

CONDITIONS = ("context_size", "threads", "threads_batch", "slots", "cache_policy")


def response(raw, binding, keys, *, request_id=None):
    reason = "lab_parameter_contract_invalid"
    try:
        value = require_object(load_strict(raw, reason), reason)
        require_exact_keys(value, {"protocol", "server_instance_id", *keys}, reason)
        if value["protocol"] != PROTOCOL:
            raise LabProtocolError(reason)
        if value["server_instance_id"] != binding["server_instance_id"]:
            raise LabProtocolError("lab_instance_mismatch")
        if request_id is not None and value["request_id"] != request_id:
            raise LabProtocolError("lab_request_mismatch")
        return value
    except LabProtocolError as exc:
        raise PreflightError(str(exc)) from exc


class LabParameters:
    async def verify_properties(self, config):
        if config["engine"]["adapter"] != self.engine_id:
            raise PreflightError("lab_engine_mismatch")
        if getattr(self, "cache_protocol", None):
            raise PreflightError("lab_cache_protocol_unsupported")
        self.drain_seconds = config["execution"]["idle_wait_seconds"]
        if self.observation_mode is None:
            await self.discover(config)
        if self.observation_mode == "native" and self.binding is None:
            return {"observation": self.capability_evidence}
        value = await self.identity(deadline=time.monotonic() + 5)
        if (
            value["build_id"] != config["engine"]["release"]
            or value["model"]["kind"] != config["model"]["kind"]
            or value["model"]["sha256"] != config["model"]["sha256"]
            or value["model"]["bytes"] != config["model"]["bytes"]
            or value["template_sha256"] != config["model"]["template_sha256"]
        ):
            raise PreflightError("lab_service_properties_mismatch")
        return value

    async def count_template(self, config, prompt):
        deadline = time.monotonic() + 5
        if self.observation_mode is None:
            await self.verify_properties(config)
        if self.observation_mode == "native" or not self.binding["capabilities"]["token_budget"]:
            return self.native_budget(config)
        if self.binding is None:
            await self.verify_properties(config)
        await self.identity(deadline=deadline)
        body = self.request_body(config, prompt, stream=False)
        value = response(
            await self.read(
                "/lab/v1/token-budget", body, deadline=deadline, headers=self.headers()
            ),
            self.binding,
            {"input_tokens", "context_size", "template_sha256", "template_prompt_sha256"},
        )
        validate_budget(value, config)
        await self.identity(deadline=deadline)
        return {
            "input_tokens": value["input_tokens"],
            "output_budget": config["generation"]["max_tokens"],
            "template_prompt_sha256": value["template_prompt_sha256"],
            "source": "/lab/v1/token-budget",
            "verification": "verified",
            "lab_token_budget": value,
        }

    async def token_budget(self, config, prompt):
        value = await self.count_template(config, prompt)
        if value["input_tokens"] is not None and (
            value["input_tokens"] + value["output_budget"] > config["conditions"]["context_size"]
        ):
            raise PreflightError("context_budget_exceeded")
        return value

    async def verify_effective(self, config, expected_input_tokens, state):
        if (
            self.observation_mode == "native"
            or not self.binding["capabilities"]["effective_parameters"]
        ):
            return self.native_parameters(config)
        deadline = time.monotonic() + 5
        await self.identity(deadline=deadline)
        value = response(
            await self.read(
                f"/lab/v1/requests/{state.request_id}/parameters",
                deadline=deadline,
                headers=self.headers(),
            ),
            self.binding,
            {"request_id", "generation", "conditions", "auxiliary_routes"},
            request_id=state.request_id,
        )
        validate_parameters(value, config)
        if state.prompt_tokens != expected_input_tokens:
            raise PreflightError("template_token_count_mismatch")
        await self.identity(deadline=deadline)
        state.parameter_evidence = value
        return effective_evidence(value, config)


def validate_budget(value, config):
    try:
        require_int(value["input_tokens"], "lab_token_budget_invalid")
        require_int(value["context_size"], "lab_token_budget_invalid", minimum=1)
        require_sha256(value["template_prompt_sha256"], "lab_token_budget_invalid")
    except LabProtocolError as exc:
        raise PreflightError(str(exc)) from exc
    if (
        value["context_size"] != config["conditions"]["context_size"]
        or value["template_sha256"] != config["model"]["template_sha256"]
    ):
        raise PreflightError("lab_token_budget_binding_mismatch")
    if value["input_tokens"] + config["generation"]["max_tokens"] > value["context_size"]:
        raise PreflightError("context_budget_exceeded")


def validate_parameters(value, config):
    try:
        _validate(value["generation"], CONFIG["properties"]["generation"], "lab.generation")
        _validate(
            value["conditions"],
            {
                "type": "object",
                "properties": {
                    key: CONFIG["properties"]["conditions"]["properties"][key] for key in CONDITIONS
                },
                "required": list(CONDITIONS),
                "additionalProperties": False,
            },
            "lab.conditions",
        )
    except ContractError as exc:
        raise PreflightError("lab_effective_parameter_invalid") from exc
    # JSON real-valued settings may be integer or float; integer counts remain strict.
    expected = {
        "generation": config["generation"],
        "conditions": {key: config["conditions"][key] for key in CONDITIONS},
        "auxiliary_routes": {"vision": False, "speculative": False, "ngram": False},
    }
    for key, requested in expected.items():
        if not _equal(value[key], requested):
            raise PreflightError("lab_effective_parameter_mismatch")


def _equal(actual, expected):
    if type(actual) in (int, float) and type(expected) in (int, float):
        return actual == expected
    if type(actual) is not type(expected):
        return False
    if isinstance(expected, dict):
        return actual.keys() == expected.keys() and all(
            _equal(actual[key], value) for key, value in expected.items()
        )
    if isinstance(expected, list):
        return len(actual) == len(expected) and all(
            _equal(a, b) for a, b in zip(actual, expected, strict=True)
        )
    return actual == expected


def effective_evidence(value, config):
    return {
        "parameters": {
            key: {
                "requested": setting,
                "effective": value["generation"][key],
                "source": "/lab/v1/requests/ID/parameters",
                "verification": "verified",
            }
            for key, setting in config["generation"].items()
        },
        "template_count_matches_usage": True,
        "lab_parameters": value,
        "stop": {"value": config["generation"]["stop"], "source": "lab_parameters"},
        "reasoning": {
            "value": config["generation"]["reasoning_mode"],
            "source": "lab_parameters",
        },
        "cache": {"value": "disabled", "source": "lab_parameters"},
    }
