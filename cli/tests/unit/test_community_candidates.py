"""Native preparation logic under injected observations; no live engine or service."""

import json
from dataclasses import replace
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.application.types import CommandRequest
from inferyard.config import (
    community_candidates,
    community_candidates_macos,
    community_candidates_windows,
    service_binding,
)
from inferyard.provisioning import preparation_io
from inferyard.config.loader import load_config
from inferyard.provisioning.preparation_io import PreparationError
from inferyard.provisioning import windows_runtime_prepare
from tests.unit.test_community_runtime import synthetic_runtime as runtime_fixture
from tests.unit.test_device_preflight import gguf

synthetic_runtime = runtime_fixture
ROOT = Path(__file__).parents[2]


def candidate_request(tmp_path, model, *, receipt=None, engine=None, system="Windows"):
    preflight = tmp_path / "preflight.json"
    selected = dict(
        model_path=str(model),
        mode="cuda" if system == "Windows" else "metal",
        threads=2,
        context_size=4096,
        min_available_memory_bytes=1073741824,
        min_disk_bytes=5368709120,
    )
    if system == "Windows":
        selected["gpu_index"] = 0
    preflight.write_text(
        json.dumps(
            {
                "hardware": {"platform": system},
                "recommendation": {"status": "recommended", "selected": selected},
            }
        )
    )
    return CommandRequest(
        "config create",
        preflight=preflight,
        bundle_path=ROOT / "bundles/zh-smoke.json",
        output_root=tmp_path / "results",
        out=tmp_path / "candidate",
        runtime_receipt=receipt,
        engine_path=engine,
        model_repo="local",
        port=48857,
    )


@pytest.mark.parametrize("failure", ["ticks", "argv"])
def test_binding_mismatch_does_not_write(tmp_path, config_path, monkeypatch, failure):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Linux", "x64"))
    config = load_config(config_path).config.to_dict()
    ticks = iter([1, 2] if failure == "ticks" else [1, 1])
    monkeypatch.setattr(service_binding, "process_start_ticks", lambda pid: next(ticks))
    argv = [
        "engine",
        *config["engine"]["startup_args"],
        *(["--changed"] if failure == "argv" else []),
    ]
    monkeypatch.setattr(service_binding, "arguments", lambda pid: argv)
    with pytest.raises(PreparationError):
        service_binding.prepare(
            CommandRequest(
                "config bind",
                candidate=config_path,
                server_pid=123,
                endpoint_url=config["endpoint"]["url"],
                out=tmp_path / "new",
            )
        )
    assert not (tmp_path / "new").exists()


def test_community_recipes_enable_webui(tmp_path):
    digest = "0" * 64
    model = SimpleNamespace(path=str(tmp_path / "model.gguf"), sha256=digest)
    engine = SimpleNamespace(path=str(tmp_path / "llama-server"), sha256=digest)
    selected = {
        "mode": "metal",
        "threads": 4,
        "context_size": 4096,
        "min_available_memory_bytes": 1 << 30,
        "min_disk_bytes": 5 << 30,
        "gpu_index": 0,
    }
    common_args = ("{{ messages }}", tmp_path / "template.jinja")
    macos_cfg = community_candidates_macos.build_config(
        selected,
        model,
        {},
        *common_args,
        tmp_path / "manifest.json",
        engine,
        "prism-b10743",
        ROOT / "bundles/zh-smoke.json",
        "1.0",
        tmp_path / "results",
        {"profile": None},
    )
    windows_cfg = community_candidates_windows.build_config(
        {**selected, "mode": "cuda"},
        model,
        {},
        *common_args,
        engine,
        {"release": "prism-b10743", "runtime_library_manifest": str(tmp_path / "manifest.json")},
        ROOT / "bundles/zh-smoke.json",
        "1.0",
        tmp_path / "results",
    )
    for cfg in (macos_cfg, windows_cfg):
        args = cfg["engine"]["startup_args"]
        assert "--no-webui" not in args
        assert not any(arg.startswith("--no-webui=") for arg in args)
        assert "--host" in args and "--port" in args
