"""Schema contract verification for standalone serve_plan documents."""

import jsonschema
import pytest

from inferyard import SCHEMA_VERSION
from inferyard.contracts.schemas import export_schema
from inferyard.contracts.validation import validate_document
from inferyard.errors import ContractError


def _valid_plan():
    return {
        "schema_version": SCHEMA_VERSION,
        "argv": ["/usr/local/bin/llama-server", "-m", "/models/qwen.gguf", "-c", "8192"],
        "environment": {},
        "working_directory": "/usr/local/bin",
        "endpoint": "http://127.0.0.1:48857",
        "model_path": "/models/qwen.gguf",
        "model_kind": "gguf",
        "context_size": 8192,
        "family": "llama-cpp",
        "adapter": "prism_llama_server_v1",
        "local": False,
    }


def _assert_valid(payload):
    jsonschema.validate(payload, export_schema("serve_plan"))
    validate_document("serve_plan", payload)


def _assert_invalid(payload):
    with pytest.raises(jsonschema.ValidationError):
        jsonschema.validate(payload, export_schema("serve_plan"))
    with pytest.raises(ContractError):
        validate_document("serve_plan", payload)


def test_serve_plan_schema_accepts_valid_documents():
    for endpoint in (
        "http://127.0.0.1:1",
        "http://127.0.0.1:48857",
        "http://localhost:65535",
        "http://[::1]:8080",
    ):
        plan = _valid_plan()
        plan["endpoint"] = endpoint
        _assert_valid(plan)

    local_plan = _valid_plan()
    local_plan.update(
        {
            "family": "polystrata",
            "adapter": "local_openai_v1",
            "model_kind": "strata_pack",
            "local": True,
            "environment": {"PYTHONDONTWRITEBYTECODE": "1"},
        }
    )
    _assert_valid(local_plan)


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("endpoint", "http://0.0.0.0:48857"),
        ("endpoint", "https://127.0.0.1:48857"),
        ("endpoint", "http://127.0.0.1:0"),
        ("endpoint", "http://127.0.0.1:01"),
        ("endpoint", "http://127.0.0.1:65536"),
        ("endpoint", "http://127.0.0.1:99999"),
        ("context_size", 0),
        ("context_size", -1),
        ("argv", []),
        ("argv", [""]),
        ("argv", ["", "-m", "/models/qwen.gguf"]),
        ("argv", ["/usr/bin/env", ""]),
        ("family", "lmstudio"),
        ("local", "false"),
        ("model_kind", "unknown"),
    ],
)
def test_serve_plan_schema_rejects_invalid_fields(field, value):
    payload = _valid_plan()
    payload[field] = value
    _assert_invalid(payload)


def test_serve_plan_schema_rejects_extra_property():
    payload = _valid_plan()
    payload["unexpected"] = True
    _assert_invalid(payload)
