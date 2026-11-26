"""Unit tests for managed local service adoption and multi-bundle host lock reuse."""

from __future__ import annotations

import json
import struct
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.config.loader import load_config
from inferyard.config.local_engines.candidate import create as create_candidate
from inferyard.contracts.validation import json_bytes
from inferyard.errors import PreflightError
from inferyard.platforms.local_engines.detect import build_installation
from inferyard.provisioning.preparation_io import PreparationError
from inferyard.runtime.local_host import admit, require_gone


def _write_gguf(path: Path, metadata: dict[str, str | int]) -> Path:
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


def _write_bundle(path: Path) -> Path:
    source = (
        Path(__file__).resolve().parents[2]
        / "src"
        / "inferyard"
        / "data"
        / "community"
        / "bundles"
        / "zh-smoke.json"
    )
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(source.read_bytes())
    return path


def _create_polystrata_layout(root: Path) -> tuple[Path, Path]:
    root.mkdir(parents=True, exist_ok=True)
    (root / "serve").mkdir(exist_ok=True)
    (root / "serve" / "server.py").write_text("print('strata server')\n", encoding="utf-8")
    (root / "setup.py").write_text("print('setup')\n", encoding="utf-8")
    venv_dir = root / ".venv"
    venv_dir.mkdir(exist_ok=True)
    (venv_dir / "pyvenv.cfg").write_text(
        "home = /usr/bin\ninclude-system-site-packages = false\n", encoding="utf-8"
    )
    exe = venv_dir / "bin" / "python"
    exe.parent.mkdir(parents=True, exist_ok=True)
    exe.write_bytes(b"#!/bin/sh\n")
    exe.chmod(0o755)
    for rel in ("engine-poly/strata-poly", "engine/strata"):
        binary = root / rel
        binary.parent.mkdir(parents=True, exist_ok=True)
        binary.write_bytes(b"synthetic-engine-binary")
        binary.chmod(0o755)
    (root / "data").mkdir(exist_ok=True)
    return root, exe


def _request(
    *,
    inst_path: Path,
    model_path: Path,
    bundle_path: Path,
    out_dir: Path,
    results_dir: Path,
    port: int,
    server_pid: int | None,
) -> SimpleNamespace:
    return SimpleNamespace(
        installation=inst_path,
        model_path=model_path,
        bundle_path=bundle_path,
        out=out_dir,
        output_root=results_dir,
        context_size=8192,
        served_model=None,
        port=port,
        server_pid=server_pid,
        model_repo=None,
        model_revision=None,
        max_tokens=512,
        timeout_seconds=600,
        prompt_cache=None,
    )


def test_polystrata_candidate_adopts_running_managed_service(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr("platform.system", lambda: "Darwin")
    monkeypatch.setattr("platform.machine", lambda: "arm64")
    monkeypatch.setattr(
        "inferyard.config.local_engines.candidate.environment_snapshot",
        lambda: {"ac_online": True},
    )

    ps_root, exe = _create_polystrata_layout(tmp_path / "polystrata")
    inst_path = tmp_path / "inst.json"
    inst_path.write_bytes(json_bytes(build_installation("polystrata", exe)))
    bundle = _write_bundle(tmp_path / "bundle.json")
    gguf_meta = {
        "general.name": "Qwen3.6-35B",
        "general.architecture": "qwen35moe",
        "tokenizer.chat_template": "{{ messages }}",
    }
    model = _write_gguf(tmp_path / "qwen36.gguf", gguf_meta)
    other_model = _write_gguf(tmp_path / "other.gguf", gguf_meta)

    running_cfg = tmp_path / "serve-live.json"
    running_cfg.write_text(
        json.dumps(
            {
                "gguf_path": str(model.resolve()),
                "context": 16384,
                "model_name": "qwen36-live",
            }
        ),
        encoding="utf-8",
    )
    live_argv = [
        str(exe),
        str((ps_root / "serve" / "server.py").resolve()),
        "--config",
        str(running_cfg),
        "--host",
        "127.0.0.1",
        "--port",
        "49100",
    ]
    monkeypatch.setattr(
        "inferyard.config.local_engines.candidate._observe_managed_process",
        lambda pid, system: (live_argv, exe.resolve(), ps_root.resolve()),
    )

    out_dir = tmp_path / "cand-adopted"
    result = create_candidate(
        _request(
            inst_path=inst_path,
            model_path=model,
            bundle_path=bundle,
            out_dir=out_dir,
            results_dir=tmp_path / "results",
            port=49100,
            server_pid=4242,
        )
    )
    assert result["candidate"] == str(out_dir / "candidate.toml")
    assert not (out_dir / "model-assets.json").exists()
    assert not (out_dir / "strata-candidate.json").exists()
    loaded = load_config(out_dir / "candidate.toml").config.to_dict()
    assert loaded["conditions"]["context_size"] == 16384
    assert loaded["engine"]["served_model"] == "qwen36-live"
    assert loaded["engine"]["polystrata_config_path"] == str(running_cfg.resolve())
    assert loaded["endpoint"]["url"] == "http://127.0.0.1:49100"

    with pytest.raises(PreparationError, match="local_engine_model_argument_mismatch"):
        create_candidate(
            _request(
                inst_path=inst_path,
                model_path=other_model,
                bundle_path=bundle,
                out_dir=tmp_path / "cand-mismatch",
                results_dir=tmp_path / "results",
                port=49100,
                server_pid=4242,
            )
        )


def test_llama_cpp_candidate_adopts_running_managed_service(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr("platform.system", lambda: "Darwin")
    monkeypatch.setattr("platform.machine", lambda: "arm64")
    monkeypatch.setattr(
        "inferyard.config.local_engines.candidate.environment_snapshot",
        lambda: {"ac_online": True},
    )

    bin_dir = tmp_path / "llama-bin"
    bin_dir.mkdir(parents=True, exist_ok=True)
    exe = bin_dir / "llama-server"
    exe.write_bytes(b"#!/bin/sh\n")
    exe.chmod(0o755)
    inst_path = tmp_path / "llama-inst.json"
    inst_path.write_bytes(json_bytes(build_installation("llama-cpp", exe)))
    bundle = _write_bundle(tmp_path / "bundle.json")
    model = _write_gguf(
        tmp_path / "model.gguf",
        {"general.name": "Qwen", "tokenizer.chat_template": "{{ messages }}"},
    )
    live_argv = [
        str(exe.resolve()),
        "--model",
        str(model.resolve()),
        "--host",
        "127.0.0.1",
        "--port",
        "49200",
        "--ctx-size",
        "32768",
        "--parallel",
        "1",
        "-a",
        "llama-live",
        "--no-webui",
    ]
    monkeypatch.setattr(
        "inferyard.config.local_engines.candidate._observe_managed_process",
        lambda pid, system: (live_argv, exe.resolve(), bin_dir.resolve()),
    )

    out_dir = tmp_path / "llama-adopted"
    result = create_candidate(
        _request(
            inst_path=inst_path,
            model_path=model,
            bundle_path=bundle,
            out_dir=out_dir,
            results_dir=tmp_path / "results",
            port=49200,
            server_pid=5151,
        )
    )
    assert result["candidate"] == str(out_dir / "candidate.toml")
    loaded = load_config(out_dir / "candidate.toml").config.to_dict()
    assert loaded["conditions"]["context_size"] == 32768
    assert loaded["engine"]["served_model"] == "llama-live"
    assert loaded["engine"]["startup_args"] == live_argv[1:]


def test_local_host_admit_reuses_clean_service_and_require_gone_guards_dirty() -> None:
    lock = SimpleNamespace(local_service=None)
    config = {
        "model": {"local_path": "/models/m.gguf"},
        "engine": {"family": "polystrata"},
        "generation": {"temperature": 0},
        "conditions": {"context_size": 8192},
    }
    service = {"pid": 777, "start_ticks": 123456, "workers": []}
    identity = {"local_engine": {"binding": {"service": service}}}

    admit(lock, config, {"bundle_id": "zh-core"}, identity, lambda pid: 123456)
    first_scope = lock.local_service["scope_sha256"]
    admit(lock, config, {"bundle_id": "zh-svg-pelican"}, identity, lambda pid: 123456)
    assert lock.local_service["scope_sha256"] != first_scope
    assert lock.local_service["service"] == service

    with pytest.raises(PreflightError, match="previous_local_process_still_alive"):
        require_gone(service, lambda pid: 123456)

    lmstudio_cfg = {**config, "engine": {"family": "lmstudio"}}
    lmstudio_lock = SimpleNamespace(local_service=None)
    admit(lmstudio_lock, lmstudio_cfg, {"bundle_id": "zh-core"}, identity, lambda pid: 123456)
    with pytest.raises(PreflightError, match="local_scope_requires_new_service_process"):
        admit(
            lmstudio_lock,
            lmstudio_cfg,
            {"bundle_id": "zh-svg-pelican"},
            identity,
            lambda pid: 123456,
        )


def test_legacy_probe_token_budgets_remain_readable() -> None:
    from inferyard.evidence.token_budgets import DEFINITION, validate_inventory

    cfg = {"execution": {"warmup_count": 3}}
    legacy = {
        "definition": DEFINITION,
        "entries": [
            {"phase": "probe", "case_id": None, "budget": {"input_tokens": 8}},
            {"phase": "warmup", "case_id": None, "budget": {"input_tokens": 8}},
            {"phase": "formal", "case_id": "c1", "budget": {"input_tokens": 16}},
        ],
    }
    assert len(validate_inventory(legacy, cfg, ["c1"], kind="run")) == 3

