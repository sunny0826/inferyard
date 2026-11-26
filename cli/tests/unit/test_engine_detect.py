"""Tests for Conda/venv local engine detection, custom env vars, and safety floors."""

from __future__ import annotations

from pathlib import Path

import pytest

from inferyard.errors import ContractError, PreflightError
from inferyard.platforms.local_engines import installation
from inferyard.platforms.local_engines.detect import build_installation, detect_local_engines
from inferyard.platforms.local_engines.startup import validate_arguments


def test_detect_conda_and_system_site_packages_venvs(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "empty-home"))
    monkeypatch.delenv("VIRTUAL_ENV", raising=False)
    monkeypatch.delenv("CONDA_PREFIX", raising=False)

    conda_env = tmp_path / "miniforge-vllm"
    (conda_env / "conda-meta").mkdir(parents=True)
    conda_py = conda_env / "bin" / "python"
    conda_py.parent.mkdir(parents=True)
    conda_py.write_bytes(b"#!/bin/sh\n")
    conda_py.chmod(0o755)
    vllm_site = conda_env / "lib" / "python3.12" / "site-packages" / "vllm"
    vllm_site.mkdir(parents=True)
    (vllm_site / "__init__.py").write_text("", encoding="utf-8")

    shared_venv = tmp_path / "system-site-mlx"
    shared_venv.mkdir()
    (shared_venv / "pyvenv.cfg").write_text(
        "home = /usr/bin\ninclude-system-site-packages = true\n", encoding="utf-8"
    )
    venv_py = shared_venv / "bin" / "python3"
    venv_py.parent.mkdir(parents=True)
    venv_py.write_bytes(b"#!/bin/sh\n")
    venv_py.chmod(0o755)
    mlx_site = shared_venv / "lib" / "python3.12" / "site-packages" / "mlx_lm"
    mlx_site.mkdir(parents=True)
    (mlx_site / "__init__.py").write_text("", encoding="utf-8")

    detected = detect_local_engines(extra_paths=(str(conda_env), str(shared_venv)))
    families = {(item["family"], Path(item["working_directory"])) for item in detected["candidates"]}
    assert ("vllm", conda_env.resolve()) in families
    assert ("mlx-lm", shared_venv.resolve()) in families

    built = build_installation("vllm", conda_py)
    assert "HF_HUB_OFFLINE" not in built["environment"]
    assert "TRANSFORMERS_OFFLINE" not in built["environment"]


def test_custom_env_vars_allowed_while_plaintext_credentials_blocked(tmp_path: Path) -> None:
    exe = tmp_path / "llama-server"
    exe.write_bytes(b"binary")

    decl = build_installation("llama-cpp", exe)
    decl["environment"] = {
        "HF_ENDPOINT": "https://hf-mirror.com",
        "CUDA_VISIBLE_DEVICES": "0,1",
    }
    validated = installation.description(decl)
    assert validated["environment"]["HF_ENDPOINT"] == "https://hf-mirror.com"

    bad_decl = dict(decl, environment={"OPENAI_API_KEY": "sk-live-secret"})
    with pytest.raises(ContractError, match="plaintext credentials"):
        installation.description(bad_decl)


def test_unknown_cli_flags_allowed_and_credential_flags_blocked(tmp_path: Path) -> None:
    model_dir = tmp_path / "model-dir"
    model_dir.mkdir()
    resolved_model = str(model_dir.resolve())

    cfg = {
        "engine": {
            "family": "vllm",
            "working_directory": str(tmp_path.resolve()),
            "startup_args": [
                "-m",
                "vllm.entrypoints.openai.api_server",
                "--model",
                resolved_model,
                "--max-model-len",
                "8192",
                "--enable-prefix-caching",
                "--kv-cache-dtype",
                "fp8",
            ],
        },
        "model": {"local_path": resolved_model},
        "conditions": {"context_size": 8192},
    }
    validate_arguments(cfg)

    cfg_cred = {
        **cfg,
        "engine": {
            **cfg["engine"],
            "startup_args": [*cfg["engine"]["startup_args"], "--api-key", "secret123"],
        },
    }
    with pytest.raises(PreflightError, match="engine_credentials_forbidden"):
        validate_arguments(cfg_cred)
