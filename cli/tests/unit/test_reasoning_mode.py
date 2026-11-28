"""Unit tests for configurable reasoning_mode across Prism and local engines."""

from __future__ import annotations

import importlib.util
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.adapters.local_engines.adapter import LocalEngineAdapter
from inferyard.cli.arguments import ArgumentError
from inferyard.cli.arguments import parser as build_parser
from inferyard.cli.request import build_request
from inferyard.config.community_candidates import resolve_max_tokens
from inferyard.config.community_candidates_macos import build_config as build_macos_config
from inferyard.config.community_candidates_windows import build_config as build_windows_config
from inferyard.config.loader import load_config
from inferyard.config.local_engines.candidate import create as create_candidate
from inferyard.contracts.validation import json_bytes
from inferyard.platforms.local_engines.detect import build_installation
from inferyard.provisioning.preparation_io import PreparationError
from tests.unit.test_thinking_and_worker_rss import _setup_polystrata, _write_bundle, _write_gguf

LOCAL_MLX_SCRIPT = Path(__file__).parents[2] / "src/inferyard/data/local_engine/mlx_server.py"


def _load_local_mlx():
    spec = importlib.util.spec_from_file_location("test_local_mlx_reasoning", LOCAL_MLX_SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_cli_request_rejects_conflicting_reasoning_flags(tmp_path: Path) -> None:
    bundle = tmp_path / "b.json"
    inst = tmp_path / "inst.json"
    model = tmp_path / "m.gguf"
    out = tmp_path / "out"
    results = tmp_path / "results"
    with pytest.raises(ArgumentError, match="cannot be used together"):
        build_request(
            build_parser().parse_args(
                [
                    "config",
                    "create",
                    "--installation",
                    str(inst),
                    "--model",
                    str(model),
                    "--bundle",
                    str(bundle),
                    "--results",
                    str(results),
                    "--out",
                    str(out),
                    "--reasoning-effort",
                    "low",
                    "--reasoning-mode",
                    "on",
                ]
            )
        )
    with pytest.raises(ArgumentError, match="context-speed profile requires reasoning off"):
        build_request(
            build_parser().parse_args(
                [
                    "config",
                    "create",
                    "--workspace",
                    str(tmp_path / "ws"),
                    "--bundle",
                    str(bundle),
                    "--engine",
                    str(tmp_path / "llama-server"),
                    "--profile",
                    "context-speed",
                    "--context-size",
                    "65536",
                    "--out",
                    str(out),
                    "--reasoning-mode",
                    "on",
                ]
            )
        )


def test_local_engine_reasoning_mode_candidate_and_request_body(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr("platform.system", lambda: "Darwin")
    monkeypatch.setattr("platform.machine", lambda: "arm64")
    monkeypatch.setattr(
        "inferyard.config.local_engines.candidate.environment_snapshot",
        lambda: {"ac_online": True},
    )

    llama_bin = tmp_path / "bin" / "llama-server"
    llama_bin.parent.mkdir(parents=True)
    llama_bin.write_bytes(b"#!/bin/sh\n")
    llama_bin.chmod(0o755)
    inst_path = tmp_path / "llama-inst.json"
    inst_path.write_bytes(json_bytes(build_installation("llama-cpp", llama_bin)))
    bundle = _write_bundle(tmp_path / "bundle.json")
    gguf = _write_gguf(
        tmp_path / "qwen3.gguf",
        {
            "general.name": "Qwen3-4B",
            "tokenizer.chat_template": "{{ messages }}",
        },
    )

    out_on = tmp_path / "cand-on"
    create_candidate(
        SimpleNamespace(
            installation=inst_path,
            model_path=gguf,
            bundle_path=bundle,
            out=out_on,
            output_root=tmp_path / "results",
            context_size=8192,
            served_model=None,
            port=48857,
            server_pid=None,
            model_repo=None,
            model_revision=None,
            max_tokens=None,
            timeout_seconds=None,
            prompt_cache=None,
            reasoning_effort=None,
            reasoning_mode="on",
        )
    )
    loaded_on = load_config(out_on / "candidate.toml").config.to_dict()
    assert loaded_on["generation"]["reasoning_mode"] == "on"
    assert loaded_on["generation"]["max_tokens"] == 4096
    body_on = LocalEngineAdapter.request_body(loaded_on, "你好")
    assert body_on["chat_template_kwargs"] == {"enable_thinking": True}

    out_off = tmp_path / "cand-off"
    create_candidate(
        SimpleNamespace(
            installation=inst_path,
            model_path=gguf,
            bundle_path=bundle,
            out=out_off,
            output_root=tmp_path / "results",
            context_size=8192,
            served_model=None,
            port=48857,
            server_pid=None,
            model_repo=None,
            model_revision=None,
            max_tokens=None,
            timeout_seconds=None,
            prompt_cache=None,
            reasoning_effort=None,
            reasoning_mode="off",
        )
    )
    loaded_off = load_config(out_off / "candidate.toml").config.to_dict()
    assert loaded_off["generation"]["reasoning_mode"] == "off"
    assert loaded_off["generation"]["max_tokens"] == 512
    body_off = LocalEngineAdapter.request_body(loaded_off, "你好")
    assert body_off["chat_template_kwargs"] == {"enable_thinking": False}

    # PolyStrata rejects reasoning_mode (uses reasoning_effort instead).
    _, ps_inst, _ = _setup_polystrata(tmp_path)
    with pytest.raises(PreparationError) as exc_info:
        create_candidate(
            SimpleNamespace(
                installation=ps_inst,
                model_path=gguf,
                bundle_path=bundle,
                out=tmp_path / "cand-ps-bad",
                output_root=tmp_path / "results",
                context_size=8192,
                served_model=None,
                port=48857,
                server_pid=None,
                model_repo=None,
                model_revision=None,
                max_tokens=None,
                timeout_seconds=None,
                prompt_cache=None,
                reasoning_effort=None,
                reasoning_mode="on",
            )
        )
    assert exc_info.value.reason == "reasoning_mode_unsupported"


def test_prism_build_config_respects_reasoning_mode(tmp_path: Path) -> None:
    asset = SimpleNamespace(path=str(tmp_path / "model.gguf"), sha256="a" * 64)
    engine = SimpleNamespace(path=str(tmp_path / "llama-server"), sha256="b" * 64)
    current = {
        "mode": "metal",
        "threads": 4,
        "context_size": 4096,
        "min_available_memory_bytes": 1024,
        "min_disk_bytes": 1024,
    }
    mac_cfg = build_macos_config(
        current,
        asset,
        {"general.name": "Qwen3-4B"},
        "{{ messages }}",
        tmp_path / "tpl.jinja",
        tmp_path / "manifest.json",
        engine,
        "b1",
        tmp_path / "bundle.json",
        "1.0.0",
        tmp_path / "results",
        {"profile": None},
        max_tokens=resolve_max_tokens({"cases": []}, 4096, reasoning_mode="on"),
        reasoning_mode="on",
    )
    assert mac_cfg["generation"]["reasoning_mode"] == "on"
    assert mac_cfg["generation"]["max_tokens"] == 3840
    assert "--reasoning" in mac_cfg["engine"]["startup_args"]
    idx = mac_cfg["engine"]["startup_args"].index("--reasoning")
    assert mac_cfg["engine"]["startup_args"][idx + 1] == "on"
    kw_idx = mac_cfg["engine"]["startup_args"].index("--chat-template-kwargs")
    assert mac_cfg["engine"]["startup_args"][kw_idx + 1] == '{"enable_thinking":true}'

    win_cfg = build_windows_config(
        {**current, "mode": "cpu"},
        asset,
        {"general.name": "Qwen3-4B"},
        "{{ messages }}",
        tmp_path / "tpl.jinja",
        engine,
        {"release": "b1", "runtime_library_manifest": str(tmp_path / "manifest.json")},
        tmp_path / "bundle.json",
        "1.0.0",
        tmp_path / "results",
        max_tokens=2048,
        reasoning_mode="on",
    )
    assert win_cfg["generation"]["reasoning_mode"] == "on"
    w_idx = win_cfg["engine"]["startup_args"].index("--reasoning")
    assert win_cfg["engine"]["startup_args"][w_idx + 1] == "on"


def test_local_mlx_server_accepts_chat_template_kwargs() -> None:
    mlx = _load_local_mlx()
    base = {
        "model": "m",
        "messages": [{"role": "user", "content": "hi"}],
        "max_tokens": 16,
        "temperature": 0,
        "stream": True,
        "stream_options": {"include_usage": True},
    }
    prompt, tokens, stream, thinking = mlx.validate_request(
        {**base, "chat_template_kwargs": {"enable_thinking": True}}, "m"
    )
    assert (prompt, tokens, stream, thinking) == ("hi", 16, True, True)
    _, _, _, thinking_off = mlx.validate_request(
        {**base, "chat_template_kwargs": {"enable_thinking": False}}, "m"
    )
    assert thinking_off is False
    _, _, _, thinking_none = mlx.validate_request(base, "m")
    assert thinking_none is None
    with pytest.raises(ValueError, match="invalid_request"):
        mlx.validate_request({**base, "chat_template_kwargs": {"enable_thinking": "yes"}}, "m")
    with pytest.raises(ValueError, match="invalid_request"):
        mlx.validate_request({**base, "chat_template_kwargs": {"other": True}}, "m")


def test_mlx_svg_reasoning_mode_on_reserves_prompt_headroom(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr("platform.system", lambda: "Darwin")
    monkeypatch.setattr("platform.machine", lambda: "arm64")
    monkeypatch.setattr(
        "inferyard.config.local_engines.candidate.environment_snapshot",
        lambda: {"ac_online": True},
    )

    venv_py = tmp_path / "mlx-venv" / "bin" / "python"
    venv_py.parent.mkdir(parents=True)
    venv_py.write_bytes(b"#!/bin/sh\n")
    venv_py.chmod(0o755)
    (tmp_path / "mlx-venv" / "pyvenv.cfg").write_text("home = /usr/bin\n", encoding="utf-8")
    inst_path = tmp_path / "mlx-inst.json"
    inst_path.write_bytes(json_bytes(build_installation("mlx-lm", venv_py)))

    model_dir = tmp_path / "qwen3-mlx"
    model_dir.mkdir()
    (model_dir / "tokenizer_config.json").write_text(
        '{"chat_template": "{{ messages }}"}', encoding="utf-8"
    )
    svg_bundle = _write_bundle(tmp_path / "zh-svg-pelican.json", name="zh-svg-pelican.json")

    out_dir = tmp_path / "cand-mlx-svg"
    create_candidate(
        SimpleNamespace(
            installation=inst_path,
            model_path=model_dir,
            bundle_path=svg_bundle,
            out=out_dir,
            output_root=tmp_path / "results",
            context_size=None,
            served_model=None,
            port=48857,
            server_pid=None,
            model_repo=None,
            model_revision=None,
            max_tokens=None,
            timeout_seconds=None,
            prompt_cache=None,
            reasoning_effort=None,
            reasoning_mode="on",
        )
    )
    loaded = load_config(out_dir / "candidate.toml").config.to_dict()
    assert loaded["conditions"]["context_size"] == 8192
    assert loaded["generation"]["reasoning_mode"] == "on"
    assert loaded["generation"]["max_tokens"] == 7936
    assert loaded["generation"]["max_tokens"] < loaded["conditions"]["context_size"]
