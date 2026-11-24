import pytest

from inferyard.errors import PreflightError

BASE = ["--no-cache-prompt", "--no-cache-idle-slots", "--cache-ram", "0"]


@pytest.mark.parametrize(
    "backend,layers,accepted",
    [
        ("cuda", "999", True),
        ("cuda", "0", False),
        ("metal", "99", True),
        ("metal", "0", False),
        ("metal", "-1", False),
        ("metal", "auto", False),
        ("cpu", "999", False),
    ],
)
def test_requested_accelerator_backend_requires_gpu_offload(
    config_path, tmp_path, backend, layers, accepted
):
    import asyncio
    import hashlib

    from inferyard.adapters.prism import BUILD, PrismAdapter
    from inferyard.config.loader import load_config

    config = load_config(config_path).config.to_dict()
    config["engine"]["backend"] = backend
    config["conditions"]["cache_policy"] = "disabled"
    config["engine"]["startup_args"] = [
        "-ngl",
        layers,
        "-t",
        str(config["conditions"]["threads"]),
        "-tb",
        str(config["conditions"]["threads_batch"]),
        "--reasoning",
        config["generation"]["reasoning_mode"],
        *BASE,
    ]
    config["model"]["template_sha256"] = hashlib.sha256(b"template").hexdigest()
    template_path = tmp_path / "template.jinja"
    template_path.write_bytes(b"template")
    config["model"]["template_path"] = str(template_path)

    async def management(path):
        return {
            "build_info": BUILD,
            "total_slots": 1,
            "chat_template": "template",
            "model_path": config["model"]["local_path"],
            "default_generation_settings": {"n_ctx": config["conditions"]["context_size"]},
        }

    adapter = object.__new__(PrismAdapter)
    adapter.management = management
    if accepted:
        asyncio.run(adapter.verify_properties(config))
    else:
        with pytest.raises(PreflightError):
            asyncio.run(adapter.verify_properties(config))
