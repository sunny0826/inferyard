"""Tests for Conda/venv local engine detection, custom env vars, and safety floors."""

from __future__ import annotations

from pathlib import Path

import pytest

from inferyard.adapters.engine_fit_common import common_service
from inferyard.config.comparison_plan_safety import comparison_safety
from inferyard.config.loader import load_config
from inferyard.config.local_engines.launch import description as launch_description
from inferyard.errors import ContractError, PreflightError
from inferyard.platforms.engine_fit_entrypoints import startup_binding
from inferyard.platforms.local_engines import installation
from inferyard.platforms.local_engines.detect import build_installation, detect_local_engines
from inferyard.platforms.local_engines.startup import validate_arguments, validate_startup


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
    conda_vllm_cli = conda_env / "bin" / "vllm"
    conda_vllm_cli.write_bytes(b"#!/bin/sh\n")
    conda_vllm_cli.chmod(0o755)
    vllm_site = conda_env / "lib" / "python3.12" / "site-packages" / "vllm"
    vllm_site.mkdir(parents=True)
    (vllm_site / "__init__.py").write_text("", encoding="utf-8")

    base_py_root = tmp_path / "BasePython"
    base_py_bin = base_py_root / "bin"
    base_py_bin.mkdir(parents=True)
    base_mlx = base_py_root / "lib" / "python3.12" / "site-packages" / "mlx_lm"
    base_mlx.mkdir(parents=True)
    (base_mlx / "__init__.py").write_text("", encoding="utf-8")
    other_minor_sglang = base_py_root / "lib" / "python3.11" / "site-packages" / "sglang"
    other_minor_sglang.mkdir(parents=True)
    (other_minor_sglang / "__init__.py").write_text("", encoding="utf-8")

    shared_venv = tmp_path / "system-site-mlx"
    shared_venv.mkdir()
    (shared_venv / "pyvenv.cfg").write_text(
        f"home = {base_py_bin}\ninclude-system-site-packages = true\nversion = 3.12.7\n",
        encoding="utf-8",
    )
    venv_py = shared_venv / "bin" / "python3"
    venv_py.parent.mkdir(parents=True)
    venv_py.write_bytes(b"#!/bin/sh\n")
    venv_py.chmod(0o755)
    (shared_venv / "lib" / "python3.12" / "site-packages").mkdir(parents=True)

    detected = detect_local_engines(extra_paths=(str(conda_vllm_cli), str(shared_venv)))
    by_family = {
        (item["family"], Path(item["working_directory"])): Path(item["executable"])
        for item in detected["candidates"]
    }
    assert by_family[("vllm", conda_env.resolve())] == conda_py.resolve()
    assert by_family[("mlx-lm", shared_venv.resolve())] == venv_py.resolve()
    assert ("sglang", shared_venv.resolve()) not in by_family

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
        "TOKENIZERS_PARALLELISM": "false",
        "MAX_TOKENS": "4096",
    }
    validated = installation.description(decl)
    assert validated["environment"]["TOKENIZERS_PARALLELISM"] == "false"

    for bad_env in (
        {"OPENAI_API_KEY": "sk-live-secret"},
        {"PGPASSWORD": "db-password-123"},
        {"HF_TOKEN": "hf_secret_token"},
        {"HTTPS_PROXY": "http://user:pass@proxy.example.com:8080"},
        {"HF_ENDPOINT": "https://token@hf-mirror.com"},
    ):
        with pytest.raises(ContractError, match="plaintext credentials"):
            installation.description({**decl, "environment": bad_env})


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


def test_legacy_bootstrap_upgrade_and_mixed_policy_regressions(tmp_path: Path) -> None:
    py_exe = tmp_path / "python3"
    py_exe.write_bytes(b"#!/bin/sh\n")
    model_dir = tmp_path / "model"
    model_dir.mkdir()
    resolved_cwd = str(tmp_path.resolve())
    resolved_exe = str(py_exe.resolve())
    resolved_model = str(model_dir.resolve())

    legacy_cfg = {
        "engine": {
            "family": "vllm",
            "working_directory": resolved_cwd,
            "environment": {},
            "startup_args": [
                "-I",
                "-S",
                "-B",
                "/old/site-packages/inferyard/data/local_engine/bootstrap.py",
                "vllm",
                "--model",
                resolved_model,
                "--max-model-len",
                "4096",
            ],
        },
        "model": {"local_path": resolved_model},
        "conditions": {"context_size": 4096},
    }
    decl = {"executable": resolved_exe, "working_directory": resolved_cwd}
    described = launch_description(legacy_cfg, decl)
    assert described["argv"][:3] == [
        resolved_exe,
        "-m",
        "vllm.entrypoints.openai.api_server",
    ]
    validate_startup(legacy_cfg, decl, described["argv"], Path(resolved_exe), Path(resolved_cwd))

    merged_safety = comparison_safety(
        [
            {
                "safety": {
                    "interval_seconds": 1,
                    "max_temperature_celsius": 85,
                    "require_temperature": False,
                    "check_environment": True,
                }
            },
            {
                "safety": {
                    "interval_seconds": 2,
                    "max_temperature_celsius": 90,
                    "require_temperature": False,
                    "check_environment": False,
                }
            },
        ]
    )
    assert merged_safety is not None and merged_safety["check_environment"] is True

    gguf = tmp_path / "m.gguf"
    gguf.write_bytes(b"GGUF")
    for bad_argv in (
        ["llama-server", "-m", str(gguf), "--api-key", "secret123"],
        ["llama-server", "-m", str(gguf), "--otlp=https://user:pass@telemetry.example.com"],
        ["llama-server", "-m", str(gguf), "https://token@telemetry.example.com"],
    ):
        with pytest.raises(PreflightError, match="engine_credentials_forbidden"):
            startup_binding("llama-cpp", bad_argv, Path("/usr/bin/llama-server"), tmp_path)

    cred_toml = tmp_path / "cred-url.toml"
    cred_toml.write_text(
        (Path(__file__).parents[1] / "fixtures/config/valid.toml")
        .read_text(encoding="utf-8")
        .replace(
            "startup_args = []",
            'startup_args = ["--telemetry-url=https://user:pass@telemetry.example.com"]',
        ),
        encoding="utf-8",
    )
    with pytest.raises(ContractError, match="credential arguments"):
        load_config(cred_toml)

    mlx_meta = common_service(
        "mlx-lm",
        {
            "engine_fit_protocol": "mlx-lm.v1",
            "engine_fit_source_sha256": "0" * 64,
            "version": "0.21.1",
        },
        "qwen",
        RuntimeError,
    )
    assert mlx_meta["version"] == "0.21.1"
