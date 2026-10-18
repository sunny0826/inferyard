import asyncio
import hashlib

import httpx
import pytest

from inferyard.adapters.prism import BUILD, PrismAdapter
from inferyard.config.loader import load_config
from inferyard.platforms.identity import PreflightError


def verify(config_path, tmp_path, raw, service, *, changed=None, build=BUILD):
    config = load_config(config_path).config.to_dict()
    path = tmp_path / "template.jinja"
    config["model"]["template_path"] = str(path)
    path.write_bytes(raw if changed is None else changed)
    config["model"]["template_sha256"] = hashlib.sha256(raw).hexdigest()
    config["engine"]["backend"] = "cpu"
    config["conditions"]["cache_policy"] = "disabled"
    config["engine"]["startup_args"] = [
        "-ngl",
        "0",
        "-t",
        str(config["conditions"]["threads"]),
        "-tb",
        str(config["conditions"]["threads_batch"]),
        "--reasoning",
        config["generation"]["reasoning_mode"],
        "--no-cache-prompt",
        "--no-cache-idle-slots",
        "--cache-ram",
        "0",
    ]
    props = {
        "build_info": build,
        "total_slots": 1,
        "default_generation_settings": {"n_ctx": config["conditions"]["context_size"]},
        "model_path": config["model"]["local_path"],
        "chat_template": service,
    }

    async def run():
        adapter = PrismAdapter(
            "http://127.0.0.1:8080",
            transport=httpx.MockTransport(lambda _: httpx.Response(200, json=props)),
        )
        try:
            return await adapter.verify_properties(config)
        finally:
            await adapter.close()

    result = asyncio.run(run())
    assert path.read_bytes() == raw
    return result


@pytest.mark.parametrize(
    "raw,service",
    [
        (b"{{ messages }}", "{{ messages }}"),
        (b"{{ messages }}\n", "{{ messages }}"),
        (b"{{ messages }}\n\n", "{{ messages }}\n"),
        (b"a\r\nb\r\n", "a\nb"),
        (b"a\rb\r", "a\nb\n"),
        (b" a\t \n", " a\t "),
    ],
)
def test_pinned_prism_lexer_source_matches_frozen_raw_bytes(config_path, tmp_path, raw, service):
    assert verify(config_path, tmp_path, raw, service)["chat_template"] == service


@pytest.mark.parametrize(
    "raw,service,changed",
    [
        (b"a\n", "b", None),
        (b"a\n\n", "a", None),
        (b" a \n", "a", None),
        (b"a\n", "a\n", None),
        (b"a\n", None, None),
        (b"a\n", "a", b"a"),
        (b"a\r\n", "a", b"a\n"),
    ],
)
def test_template_changes_remain_rejected(config_path, tmp_path, raw, service, changed):
    with pytest.raises(PreflightError, match="service_template_mismatch"):
        verify(config_path, tmp_path, raw, service, changed=changed)


def test_unknown_build_cannot_use_pinned_lexer_rule(config_path, tmp_path):
    with pytest.raises(PreflightError, match="unsupported_engine_build"):
        verify(config_path, tmp_path, b"a\n", "a", build="unknown")
