"""Service binding refusal and Darwin candidate engine release tests."""

import json
import struct
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.config import community_candidates, service_binding
from inferyard.config.loader import load_config
from inferyard.platforms import identity as platform_identity
from inferyard.provisioning import preparation_io, runtime_verification
from inferyard.provisioning.preparation_io import PreparationError

ROOT = Path(__file__).parents[2]


def _write_gguf(path: Path) -> Path:
    metadata = {
        "general.architecture": "llama",
        "general.name": "Qwen Test",
        "llama.context_length": 16384,
        "tokenizer.chat_template": "{{ messages[0]['content'] }}\n",
    }
    parts = [struct.pack("<4sIQQ", b"GGUF", 3, 1, len(metadata))]
    for key, value in metadata.items():
        key_bytes = key.encode("utf-8")
        parts.append(struct.pack("<Q", len(key_bytes)) + key_bytes)
        if isinstance(value, str):
            val_bytes = value.encode("utf-8")
            parts.append(struct.pack("<IQ", 8, len(val_bytes)) + val_bytes)
        else:
            parts.append(struct.pack("<II", 4, value))
    parts.append(b"tensors")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(b"".join(parts))
    return path


def candidate_request(
    tmp_path,
    model,
    *,
    receipt=None,
    engine=None,
    system="Windows",
    bundle_name="zh-smoke",
    context_size=8192,
    out_name="candidate",
):
    preflight = tmp_path / f"preflight-{out_name}.json"
    selected = dict(
        model_path=str(model),
        mode="cuda" if system == "Windows" else "metal",
        threads=2,
        context_size=context_size,
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
        bundle_path=ROOT.parent / f"benchmarks/bundles/{bundle_name}.json",
        output_root=tmp_path / "results",
        out=tmp_path / out_name,
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


@pytest.mark.parametrize(
    "version_output,expected_release",
    [
        ("llama-server prism-b12232-f702476\n", "prism-b12232-f702476"),
        ("version: 12232 (f70247691)\n", "b12232-f70247691"),
    ],
)
def test_darwin_candidate_records_resolved_llama_server_release(
    tmp_path, monkeypatch, version_output, expected_release
):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Darwin", "arm64"))
    monkeypatch.setattr(community_candidates, "fresh_macos", lambda selected, results: selected)
    monkeypatch.setattr(
        platform_identity,
        "environment_snapshot",
        lambda: {"profile": "balanced", "macos_power_policy": None},
    )
    monkeypatch.setattr(
        runtime_verification.subprocess,
        "run",
        lambda *a, **k: type(
            "Result", (), {"returncode": 0, "stdout": version_output, "stderr": ""}
        )(),
    )
    model = _write_gguf(tmp_path / "models" / "qwen.gguf")
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    engine = bin_dir / "llama-server"
    engine.write_bytes(b"mach-o-binary")
    (bin_dir / "libggml-metal.dylib").write_bytes(b"mach-o-dylib")

    request = candidate_request(tmp_path, model, engine=engine, system="Darwin")
    details = community_candidates.create(request)
    loaded = load_config(Path(details["candidate"])).config.to_dict()
    assert loaded["engine"]["release"] == expected_release
    assert loaded["engine"]["adapter"] == "prism_llama_server_v1"


def test_device_preflight_recommends_8k_context_within_model_limit():
    from inferyard.platforms.device_preflight import GIB, recommend

    hardware = {
        "platform": "Darwin",
        "apple_silicon": True,
        "logical_cpus": 14,
        "memory_available_bytes": 32 * GIB,
        "disk_free_bytes": 50 * GIB,
        "gpu": {"devices": [{"metal_supported": True, "name": "Apple M5 Pro"}]},
    }

    def model_entry(path: str, ctx: int | None) -> dict:
        return {
            "path": path,
            "name": Path(path).name,
            "status": "available",
            "chat_template_available": True,
            "size_bytes": 4 * GIB,
            "context_length": ctx,
        }

    rec_bonsai = recommend(hardware, [model_entry("/m/bonsai.gguf", 262144)])
    assert rec_bonsai["selected"]["context_size"] == 8192

    rec_4k = recommend(hardware, [model_entry("/m/small-ctx.gguf", 4096)])
    assert rec_4k["selected"]["context_size"] == 4096

    rec_missing = recommend(hardware, [model_entry("/m/no-ctx.gguf", None)])
    assert rec_missing["selected"]["context_size"] == 4096


@pytest.mark.parametrize("system", ["Darwin", "Windows"])
def test_community_candidates_apply_bundle_max_tokens_and_share_service_identity(
    tmp_path, monkeypatch, system
):
    arch = "arm64" if system == "Darwin" else "x64"
    monkeypatch.setattr(preparation_io, "native_platform", lambda: (system, arch))
    monkeypatch.setattr(community_candidates, "fresh_macos", lambda selected, results: selected)
    monkeypatch.setattr(
        platform_identity,
        "environment_snapshot",
        lambda: {"profile": "balanced", "macos_power_policy": None},
    )
    monkeypatch.setattr(
        runtime_verification.subprocess,
        "run",
        lambda *a, **k: type(
            "Result", (), {"returncode": 0, "stdout": "version: 12232 (f70247610)\n", "stderr": ""}
        )(),
    )
    model = _write_gguf(tmp_path / "models" / "Ternary-Bonsai-2.5-27B-PTQ1_0.gguf")
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    engine = bin_dir / ("llama-server" if system == "Darwin" else "llama-server.exe")
    engine.write_bytes(b"binary")
    receipt = None
    if system == "Darwin":
        (bin_dir / "libggml-metal.dylib").write_bytes(b"mach-o-dylib")
    else:
        receipt = tmp_path / "runtime-receipt.json"
        receipt.write_text("{}")
        manifest = tmp_path / "engine-sha256.json"
        manifest.write_text("{}")
        monkeypatch.setattr(
            community_candidates,
            "verify_receipt",
            lambda _path, mode: ({"libraries": {"ggml.dll": "0" * 64}}, engine, manifest),
        )
        monkeypatch.setattr(community_candidates, "query_version", lambda *_a: "b10743-adfffbe")

    core_req = candidate_request(
        tmp_path,
        model,
        engine=engine if system == "Darwin" else None,
        receipt=receipt,
        system=system,
        bundle_name="zh-core",
        context_size=8192,
        out_name="cand-core",
    )
    svg_req = candidate_request(
        tmp_path,
        model,
        engine=engine if system == "Darwin" else None,
        receipt=receipt,
        system=system,
        bundle_name="zh-svg-pelican",
        context_size=8192,
        out_name="cand-svg",
    )
    core_cand = Path(community_candidates.create(core_req)["candidate"])
    svg_cand = Path(community_candidates.create(svg_req)["candidate"])

    core_cfg = load_config(core_cand).config.to_dict()
    svg_cfg = load_config(svg_cand).config.to_dict()

    assert core_cfg["generation"]["max_tokens"] == 512
    assert svg_cfg["generation"]["max_tokens"] == 4096
    assert core_cfg["conditions"]["context_size"] == 8192
    assert svg_cfg["conditions"]["context_size"] == 8192
    assert core_cfg["engine"]["startup_args"] == svg_cfg["engine"]["startup_args"]

    live_argv = [str(engine), *core_cfg["engine"]["startup_args"]]
    bound_core, _ = service_binding.bind(
        core_cand, 901, "http://127.0.0.1:48857", ticks=lambda _p: 77, argv=lambda _p: live_argv
    )
    bound_svg, _ = service_binding.bind(
        svg_cand, 901, "http://127.0.0.1:48857", ticks=lambda _p: 77, argv=lambda _p: live_argv
    )
    assert bound_core["generation"]["max_tokens"] == 512
    assert bound_svg["generation"]["max_tokens"] == 4096


def test_community_candidates_reserve_prompt_headroom_on_4k_context(tmp_path, monkeypatch):
    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Darwin", "arm64"))
    monkeypatch.setattr(community_candidates, "fresh_macos", lambda selected, results: selected)
    monkeypatch.setattr(
        platform_identity,
        "environment_snapshot",
        lambda: {"profile": "balanced", "macos_power_policy": None},
    )
    monkeypatch.setattr(
        runtime_verification.subprocess,
        "run",
        lambda *a, **k: type(
            "Result", (), {"returncode": 0, "stdout": "version: 12232 (f70247610)\n", "stderr": ""}
        )(),
    )
    model = _write_gguf(tmp_path / "models" / "small-ctx.gguf")
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    engine = bin_dir / "llama-server"
    engine.write_bytes(b"mach-o-binary")
    (bin_dir / "libggml-metal.dylib").write_bytes(b"mach-o-dylib")

    req = candidate_request(
        tmp_path,
        model,
        engine=engine,
        system="Darwin",
        bundle_name="zh-svg-pelican",
        context_size=4096,
        out_name="cand-svg-4k",
    )
    cand = Path(community_candidates.create(req)["candidate"])
    cfg = load_config(cand).config.to_dict()
    assert cfg["conditions"]["context_size"] == 4096
    assert cfg["generation"]["max_tokens"] == 3840

    custom_bundle = {
        "bundle_id": "custom-non-fallback",
        "execution_hints": {"default_max_tokens": 2048},
        "cases": [{"case_id": "c1", "category": "qa"}],
    }
    assert community_candidates.resolve_max_tokens(custom_bundle, 8192) == 2048
