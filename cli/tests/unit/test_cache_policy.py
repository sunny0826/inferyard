import pytest

from inferyard.config.cache_policy import disabled_startup

BASE = ["--no-cache-prompt", "--no-cache-idle-slots", "--cache-ram", "0"]


@pytest.mark.parametrize(
    "suffix",
    [
        ["--cache-ram", "1024"],
        ["-cram", "1024"],
        ["--cache-ram=1024"],
        ["--cache-ram", "0"],
        ["--cache-prompt"],
        ["--cache-idle-slots"],
        ["--cache-prompt=true"],
        ["--no-cache-prompt"],
        ["--no-cache-idle-slots"],
    ],
)
def test_conflicting_or_duplicate_cache_controls_rejected(suffix):
    assert not disabled_startup(BASE + suffix)
    assert not disabled_startup(suffix + BASE)


@pytest.mark.parametrize(
    "tail", [["--cache-ram", "0"], ["-cram", "0"], ["--cache-ram=0"], ["-cram=0"]]
)
def test_explicit_unique_disabled_controls(tail):
    assert disabled_startup(BASE[:2] + tail)


@pytest.mark.parametrize(
    "args", [[], BASE[:2], BASE[:-1], BASE[1:], ["--no-cache-prompt=true"] + BASE[1:]]
)
def test_missing_or_malformed_controls_rejected(args):
    assert not disabled_startup(args)


def test_adapter_refuses_conflicting_ram_before_generation(config_path):
    import asyncio
    import hashlib

    from inferyard.adapters.prism import BUILD, PrismAdapter
    from inferyard.config.loader import load_config
    from inferyard.platforms.identity import PreflightError

    config = load_config(config_path).config.to_dict()
    config["conditions"]["cache_policy"] = "disabled"
    config["model"]["template_sha256"] = hashlib.sha256(b"template").hexdigest()
    config["engine"]["startup_args"] = [
        "-ngl",
        "0",
        "-t",
        str(config["conditions"]["threads"]),
        "-tb",
        str(config["conditions"]["threads_batch"]),
        "--reasoning",
        config["generation"]["reasoning_mode"],
        *BASE,
    ]

    async def management(path):
        assert path == "/props"
        return {
            "build_info": BUILD,
            "total_slots": 1,
            "default_generation_settings": {"n_ctx": config["conditions"]["context_size"]},
            "model_path": config["model"]["local_path"],
            "chat_template": "template",
        }

    adapter = object.__new__(PrismAdapter)
    adapter.management = management
    asyncio.run(adapter.verify_properties(config))
    config["engine"]["startup_args"] += ["--cache-ram", "1024"]
    with pytest.raises(PreflightError, match="cache_policy_unverified"):
        asyncio.run(adapter.verify_properties(config))


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
def test_requested_accelerator_backend_requires_gpu_offload(config_path, backend, layers, accepted):
    import asyncio
    import hashlib

    from inferyard.adapters.prism import BUILD, PrismAdapter
    from inferyard.config.loader import load_config
    from inferyard.platforms.identity import PreflightError

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
