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
