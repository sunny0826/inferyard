import asyncio
import hashlib

import httpx
import pytest

from inferyard.adapters.prism import BUILD, PrismAdapter
from inferyard.config.loader import load_config
from inferyard.errors import PreflightError


def verify(config_path, tmp_path, raw, service, *, changed=None, build=BUILD, release=None):
    config = load_config(config_path).config.to_dict()
    if release is not None:
        config["engine"]["release"] = release
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


@pytest.mark.parametrize(
    "release,build,service",
    [
        ("prism-b12232-f702476", "b12232-f702476", "a"),
        ("prism-b12232-f702476", "b12232-f70247691", "a"),
        ("prism-b12232-f702476", "b12232-f70247691", "a\n"),
        ("b12232-f70247691", "b12232-f70247691", "a\n"),
        ("prism-b10743-adfffbe", "b10743-adfffbe41", "a"),
        ("e2e-fake-engine", "b10743-adfffbe41", "a"),
    ],
)
def test_verify_properties_accepts_matching_prism_builds(
    config_path, tmp_path, release, build, service
):
    props = verify(config_path, tmp_path, b"a\n", service, build=build, release=release)
    assert props["build_info"] == build


@pytest.mark.parametrize(
    "release,build",
    [
        ("prism-b12232-f702476", "b10743-adfffbe41"),
        ("prism-b12232-f702476", "b99999-deadbeef"),
        ("prism-b10743-adfffbe", "b99999-deadbeef"),
        ("prism-b12232-f702476", "prism-b12232-f702476"),
    ],
)
def test_verify_properties_rejects_mismatched_prism_builds(config_path, tmp_path, release, build):
    with pytest.raises(PreflightError, match="unsupported_engine_build"):
        verify(config_path, tmp_path, b"a\n", "a", build=build, release=release)


def test_bound_capabilities_accepts_upgraded_prism_build_and_rejects_mismatch():
    from inferyard.evidence.engine_capabilities import bound_capabilities

    class FakeReads:
        def __init__(self, build_info, release, adapter="prism_llama_server_v1"):
            self._build_info = build_info
            self._cfg = {"engine": {"adapter": adapter, "release": release}}

        def observation(self, kind):
            assert kind == "service_properties"
            return {"build_info": self._build_info}

        def input(self, kind):
            assert kind == "config"
            return self._cfg

        def observation_reference(self, kind, *, stage=None, key=None):
            _ = kind, stage, key
            return {"path": "observations.json"}

    manifest = {"observations.json": "0" * 64}
    ok_cap = bound_capabilities(
        "/unused", manifest, reads=FakeReads("b12232-f70247691", "prism-b12232-f702476")
    )
    assert ok_cap.prism_definition is True
    assert ok_cap.verified is True

    bad_cap = bound_capabilities(
        "/unused", manifest, reads=FakeReads("b99999-deadbeef", "prism-b12232-f702476")
    )
    assert bad_cap.prism_definition is False

    upstream_cap = bound_capabilities(
        "/unused",
        manifest,
        reads=FakeReads("b11146-7fe450e19", "b11146-7fe450e19", adapter="llama_cpp_b11146_v1"),
    )
    assert upstream_cap.prism_definition is False
