"""Unit tests for PolyStrata local-engine contracts, cross-platform detection, and candidates."""

from __future__ import annotations

import hashlib
import json
import os
import struct
from copy import deepcopy
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.application.engine_check import execute as execute_engine_check
from inferyard.application.engine_detect import execute as execute_engine_detect
from inferyard.config.loader import load_config
from inferyard.config.local_engines.candidate import create as create_candidate
from inferyard.config.local_engines.inspection import describe as describe_candidate
from inferyard.contracts.engine_profiles import (
    DIAGNOSTIC_ENGINES,
    LOCAL_FAMILIES,
    POLYSTRATA_SUPPORTED_GGUF_ARCHS,
)
from inferyard.contracts.local_engines import validate_config
from inferyard.contracts.local_environment import validate_root_environment
from inferyard.contracts.schemas_engine_check import ENGINE_CHECK
from inferyard.contracts.validation import json_bytes, validate_document, validate_value
from inferyard.errors import ContractError, PreflightError
from inferyard.platforms.identity import hash_file
from inferyard.platforms.local_engines import installation, service
from inferyard.platforms.local_engines.detect import build_installation, detect_local_engines
from inferyard.platforms.local_engines.offline import validate_frozen_identity
from inferyard.platforms.local_engines.startup import validate_arguments
from inferyard.provisioning.preparation_io import PreparationError
from local_engine_helpers import local_config  # noqa: F401


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
    parts.append(b"synthetic-gguf-tensors")
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


def _create_polystrata_layout(
    root: Path,
    *,
    platform_kind: str = "macos",
    build_json: dict | None = None,
    with_data_dir: bool = True,
) -> tuple[Path, Path]:
    root.mkdir(parents=True, exist_ok=True)
    (root / "serve").mkdir(exist_ok=True)
    (root / "serve" / "server.py").write_text("print('strata server')\n", encoding="utf-8")
    (root / "setup.py").write_text("print('setup')\n", encoding="utf-8")

    venv_dir = root / ".venv"
    venv_dir.mkdir(exist_ok=True)
    (venv_dir / "pyvenv.cfg").write_text(
        "home = /usr/bin\ninclude-system-site-packages = false\n", encoding="utf-8"
    )
    if platform_kind == "windows":
        exe = venv_dir / "Scripts" / "python.exe"
        engine_bin = root / "engine-poly" / "strata-poly.exe"
    elif platform_kind == "linux":
        exe = venv_dir / "bin" / "python"
        engine_bin = root / "engine-poly" / "strata-poly"
    else:
        exe = venv_dir / "bin" / "python"
        engine_bin = root / "build-metal" / "strata-metal"
    exe.parent.mkdir(parents=True, exist_ok=True)
    exe.write_bytes(b"#!/bin/sh\n")
    exe.chmod(0o755)
    engine_bin.parent.mkdir(parents=True, exist_ok=True)
    engine_bin.write_bytes(b"synthetic-engine-binary")
    engine_bin.chmod(0o755)

    if build_json is not None:
        (root / "BUILD.json").write_text(json.dumps(build_json), encoding="utf-8")
    if with_data_dir:
        (root / "data").mkdir(exist_ok=True)
    return root, exe


def _candidate_request(
    *,
    inst_path: Path,
    model_path: Path,
    bundle_path: Path,
    out_dir: Path,
    results_dir: Path,
    context_size: int | None = None,
    port: int = 48857,
) -> SimpleNamespace:
    return SimpleNamespace(
        installation=inst_path,
        model_path=model_path,
        bundle_path=bundle_path,
        out=out_dir,
        output_root=results_dir,
        context_size=context_size,
        served_model="qwen36-local",
        port=port,
        server_pid=None,
        model_repo="unsloth/Qwen3.6-35B-A3B-MTP-GGUF",
        model_revision=None,
        max_tokens=512,
        timeout_seconds=600,
        prompt_cache=None,
        temperature_stop_override_reason=None,
    )


def test_polystrata_profiles_and_hip_schema_validation(local_config) -> None:
    assert "polystrata" in LOCAL_FAMILIES
    assert "polystrata" in DIAGNOSTIC_ENGINES
    assert "qwen35moe" in POLYSTRATA_SUPPORTED_GGUF_ARCHS

    # Cross-platform absolute paths in local_environment
    validate_root_environment(
        "sglang",
        {"PROMETHEUS_MULTIPROC_DIR": r"C:\work\sglang\prom"},
        r"C:\work\sglang",
    )
    validate_root_environment(
        "sglang",
        {"PROMETHEUS_MULTIPROC_DIR": "/work/sglang/prom"},
        "/work/sglang",
    )
    with pytest.raises(ContractError):
        validate_root_environment(
            "sglang",
            {"PROMETHEUS_MULTIPROC_DIR": r"C:\other\prom"},
            r"C:\work\sglang",
        )

    # Valid PolyStrata config with HIP backend, >32K context, require_temperature=False
    cfg = deepcopy(local_config)
    cfg["model"]["kind"] = "gguf"
    cfg["model"]["local_path"] = "/models/qwen36.gguf"
    cfg["engine"].update(
        family="polystrata",
        backend="hip",
        polystrata_config_path="/out/strata-candidate.json",
        startup_args=[
            "/opt/polystrata/serve/server.py",
            "--engine",
            "strata",
            "--config",
            "/out/strata-candidate.json",
            "--host",
            "127.0.0.1",
            "--port",
            "48857",
        ],
        environment={"PYTHONDONTWRITEBYTECODE": "1"},
    )
    cfg["generation"]["reasoning_effort"] = "none"
    cfg["conditions"]["context_size"] = 262144
    cfg["safety"]["require_temperature"] = False
    validate_document("config", cfg)
    validate_config(cfg)
    validate_arguments(cfg)

    # Studio report accepts hip backend
    report = {
        "schema_version": 3,
        "kind": "studio-report.v1",
        "runs": [
            {
                "source": {"run_id": "run-1", "path": "/runs/run-1"},
                "model": "qwen36",
                "threads": None,
                "diagnostic": False,
                "facets": {"bundle_label": "zh-smoke"},
                "profile": {
                    "title": "PolyStrata HIP",
                    "engine": {
                        "binary_path": "/opt/polystrata/.venv/bin/python",
                        "adapter": "local_openai_v1",
                        "backend": "hip",
                    },
                    "machine": {
                        "cpu": "Ryzen",
                        "logical_cpus": 16,
                        "memory_total_bytes": 64 * 1024**3,
                    },
                },
                "summary": {
                    "completion_rate": {"numerator": 1, "denominator": 1, "excluded": 0},
                    "stop_reason": "completed",
                },
                "dashboard": {
                    "quality": {
                        "numerator": 1,
                        "denominator": 1,
                        "excluded": 0,
                        "complete": True,
                        "reason": None,
                    },
                    "categories": [
                        {
                            "id": "qa",
                            "label": "QA",
                            "rate": {"numerator": 1, "denominator": 1, "excluded": 0},
                        }
                    ],
                    "latency": {
                        "sample_count": 1,
                        "excluded": 0,
                        "min": 100.0,
                        "p50": 100.0,
                        "p95": None,
                        "max": 100.0,
                    },
                    "sampled_peak_rss_bytes": None,
                    "sampled_peak_rss_reason": "unavailable",
                    "sampled_minimum_available_bytes": None,
                    "minimum_available_reason": "unavailable",
                },
                "requests": [
                    {
                        "case_id": "c1",
                        "category_label": "QA",
                        "state": "completed",
                        "quality": "pass",
                        "latency_ms": 100.0,
                    }
                ],
            }
        ],
    }
    validate_document("studio_report", report)

    # Metal backend still enforces require_temperature=True
    metal_bad = deepcopy(cfg)
    metal_bad["engine"]["backend"] = "metal"
    metal_bad["safety"]["require_temperature"] = False
    with pytest.raises(ContractError, match="require_temperature"):
        validate_config(metal_bad)

    # polystrata_config_path required for polystrata, forbidden for others
    missing_ps = deepcopy(cfg)
    missing_ps["engine"].pop("polystrata_config_path")
    with pytest.raises(ContractError, match="polystrata_config_path"):
        validate_config(missing_ps)

    bad_other = deepcopy(local_config)
    bad_other["generation"]["reasoning_effort"] = "low"
    with pytest.raises(ContractError, match="reasoning_effort"):
        validate_config(bad_other)

    # --open is rejected in polystrata startup args
    open_bad = deepcopy(cfg)
    open_bad["engine"]["startup_args"].append("--open")
    with pytest.raises(PreflightError):
        validate_arguments(open_bad)


def test_detect_polystrata_layouts_and_excludes_data_from_runtime_roots(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    home = tmp_path / "home"
    home.mkdir()
    monkeypatch.setenv("HOME", str(home))
    monkeypatch.delenv("VIRTUAL_ENV", raising=False)
    monkeypatch.delenv("CONDA_PREFIX", raising=False)
    monkeypatch.chdir(tmp_path)
    monkeypatch.setattr("inferyard.platforms.local_engines.detect.shutil.which", lambda _: None)
    monkeypatch.setattr("inferyard.platforms.local_engines.detect._STANDARD_LLAMA_PATHS", ())

    ws_ps, ws_exe = _create_polystrata_layout(
        tmp_path / "engines" / "polystrata",
        platform_kind="macos",
        build_json={"version": "v0.1.45-poly.10", "backend": "metal"},
    )
    (ws_ps / "data" / "strata-qwen36.json").write_text("{}", encoding="utf-8")

    env_ps, env_exe = _create_polystrata_layout(
        tmp_path / "custom" / "ps-win",
        platform_kind="windows",
        build_json={"version": "v0.1.45-poly.10", "backend": "cuda"},
    )
    monkeypatch.setenv("POLYSTRATA_ROOT", str(env_ps))

    result = detect_local_engines()
    ps_candidates = [c for c in result["candidates"] if c["family"] == "polystrata"]
    assert len(ps_candidates) == 2

    by_root = {c["working_directory"]: c for c in ps_candidates}
    ws_entry = by_root[str(ws_ps.resolve())]
    assert ws_entry["executable"] == str(ws_exe.resolve())
    assert ws_entry["version"] == "v0.1.45-poly.10"
    assert ws_entry["data_dir"] == str((ws_ps / "data").resolve())
    assert ws_entry["model_configs"] == [str((ws_ps / "data" / "strata-qwen36.json").resolve())]
    assert str((ws_ps / "data").resolve()) not in ws_entry["runtime_roots"]
    assert str((ws_ps / "serve").resolve()) in ws_entry["runtime_roots"]
    assert str((ws_ps / "build-metal").resolve()) in ws_entry["runtime_roots"]

    win_entry = by_root[str(env_ps.resolve())]
    assert win_entry["executable"] == str(env_exe.resolve())
    assert str((env_ps / "engine-poly").resolve()) in win_entry["runtime_roots"]


@pytest.mark.parametrize(
    ("platform_kind", "sys_name", "machine"),
    [
        ("macos", "Darwin", "arm64"),
        ("linux", "Linux", "x86_64"),
        ("windows", "Windows", "AMD64"),
    ],
)
def test_init_installation_and_engine_check_cross_platform(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    platform_kind: str,
    sys_name: str,
    machine: str,
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr(installation, "_FILE_CACHE", {})
    monkeypatch.setattr("platform.system", lambda: sys_name)
    monkeypatch.setattr("platform.machine", lambda: machine)

    ps_root, exe = _create_polystrata_layout(
        tmp_path / f"ps-{platform_kind}",
        platform_kind=platform_kind,
        build_json={"version": "v0.1.45-poly.10"},
    )
    out_json = tmp_path / f"inst-{platform_kind}.json"
    code, init_res = execute_engine_detect(
        SimpleNamespace(
            command="engine init-installation",
            family="polystrata",
            executable=exe,
            out=out_json,
            working_directory=None,
            runtime_roots=None,
            models_root=None,
            data_dir=ps_root / "data",
        )
    )
    assert code == 0, init_res
    assert init_res.details["verified"] is True
    assert init_res.details["installation"]["working_directory"] == str(ps_root.resolve())
    assert init_res.details["installation"]["data_dir"] == str((ps_root / "data").resolve())

    check_code, check_res = execute_engine_check(
        SimpleNamespace(command="engine check", installation=out_json)
    )
    assert check_code == 0
    assert check_res.details["eligible"] is True
    assert check_res.details["family"] == "polystrata"
    validate_value(
        {
            "command": "engine check",
            "status": check_res.status,
            "completeness": check_res.completeness,
            "run_id": None,
            "evidence_dir": None,
            "limitations": list(check_res.limitations),
            "details": check_res.details,
        },
        ENGINE_CHECK,
        "engine_check",
    )

    # Adding model weights under data/ does NOT invalidate the runtime snapshot
    decl = installation.read_installation(out_json)
    snapshot = installation.freeze_installation(decl)
    (ps_root / "data" / "new-model.gguf").write_bytes(b"weights")
    installation.verify_installation(snapshot)

    # Editing serve/server.py DOES trigger runtime drift
    (ps_root / "serve" / "server.py").write_text("print('tampered')\n", encoding="utf-8")
    with pytest.raises(PreflightError, match="local_engine_runtime_changed"):
        installation.verify_installation(snapshot)


def test_polystrata_layout_validation_errors(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr("platform.system", lambda: "Linux")
    monkeypatch.setattr("platform.machine", lambda: "x86_64")

    ps_root, exe = _create_polystrata_layout(tmp_path / "ps-broken", platform_kind="linux")
    decl = build_installation("polystrata", exe)

    # Missing server.py
    (ps_root / "serve" / "server.py").unlink()
    with pytest.raises(PreflightError, match="local_engine_polystrata_server_missing"):
        installation.freeze_installation(decl)
    (ps_root / "serve" / "server.py").write_text("ok\n", encoding="utf-8")

    # Missing native engine dir
    (ps_root / "engine-poly" / "strata-poly").unlink()
    (ps_root / "engine-poly").rmdir()
    decl_no_eng = build_installation(
        "polystrata",
        exe,
        working_directory=ps_root,
        runtime_roots=[ps_root / "serve", ps_root / ".venv"],
    )
    with pytest.raises(PreflightError, match="local_engine_polystrata_engine_missing"):
        installation.freeze_installation(decl_no_eng)

    # Unsupported architecture
    monkeypatch.setattr("platform.machine", lambda: "aarch64")
    with pytest.raises(PreflightError, match="local_engine_platform_unsupported"):
        installation.freeze_installation(decl_no_eng)


@pytest.mark.parametrize(
    ("platform_kind", "sys_name", "machine", "expected_backend", "expected_temp"),
    [
        ("macos", "Darwin", "arm64", "metal", True),
        ("linux", "Linux", "x86_64", "hip", False),
        ("windows", "Windows", "AMD64", "cuda", False),
    ],
)
def test_config_create_shared_gguf_and_describe_cross_platform(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    platform_kind: str,
    sys_name: str,
    machine: str,
    expected_backend: str,
    expected_temp: bool,
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr(installation, "_FILE_CACHE", {})
    monkeypatch.setattr("platform.system", lambda: sys_name)
    monkeypatch.setattr("platform.machine", lambda: machine)
    monkeypatch.setattr(
        "inferyard.config.local_engines.candidate.environment_snapshot",
        lambda: {"ac_online": True if sys_name == "Darwin" else None},
    )

    ps_root, exe = _create_polystrata_layout(
        tmp_path / f"ps-{platform_kind}",
        platform_kind=platform_kind,
        build_json={"version": "v0.1.45-poly.10", "backend": expected_backend},
    )
    inst_path = tmp_path / f"inst-{platform_kind}.json"
    decl = build_installation("polystrata", exe)
    inst_path.write_bytes(json_bytes(decl))

    gguf = _write_gguf(
        tmp_path / "models" / "qwen36-35b-a3b.gguf",
        {
            "general.name": "Qwen3.6-35B-A3B",
            "general.architecture": "qwen35moe",
            "tokenizer.chat_template": "{{ messages }}",
        },
    )
    bundle = _write_bundle(tmp_path / "bundles" / "zh-smoke.json")
    out_dir = tmp_path / f"candidate-{platform_kind}"
    results_dir = tmp_path / f"results-{platform_kind}"

    details = create_candidate(
        _candidate_request(
            inst_path=inst_path,
            model_path=gguf,
            bundle_path=bundle,
            out_dir=out_dir,
            results_dir=results_dir,
            context_size=65536,
        )
    )
    assert details["family"] == "polystrata"
    derived_cfg = Path(details["polystrata_config"])
    assert derived_cfg == out_dir.resolve() / "strata-candidate.json"
    assert derived_cfg.is_file()
    derived_data = json.loads(derived_cfg.read_text(encoding="utf-8"))
    assert derived_data["engine"] == "strata-poly"
    assert derived_data["arch"] == "qwen35moe"
    assert derived_data["gguf"] == str(gguf.resolve())
    assert derived_data["context"] == 65536
    # Weights must NOT be duplicated into the candidate output directory
    assert list(out_dir.glob("*.gguf")) == []

    loaded = load_config(details["candidate"]).config.to_dict()
    assert loaded["engine"]["family"] == "polystrata"
    assert loaded["engine"]["backend"] == expected_backend
    assert loaded["engine"]["polystrata_config_path"] == str(derived_cfg)
    assert loaded["engine"]["startup_args"] == [
        str((ps_root / "serve" / "server.py").resolve()),
        "--engine",
        "strata",
        "--config",
        str(derived_cfg),
        "--host",
        "127.0.0.1",
        "--port",
        "48857",
    ]
    assert "--open" not in loaded["engine"]["startup_args"]
    assert loaded["generation"]["reasoning_effort"] == "none"
    assert loaded["conditions"]["context_size"] == 65536
    assert loaded["safety"]["require_temperature"] is expected_temp
    assert loaded["model"]["kind"] == "gguf"
    assert loaded["model"]["local_path"] == str(gguf.resolve())

    # config describe works cross-platform
    described = describe_candidate(SimpleNamespace(candidate=Path(details["candidate"])))
    assert described["family"] == "polystrata"
    assert described["launch"]["kind"] == "direct_process"
    assert described["launch"]["argv"][0] == str(exe.resolve())
    assert described["launch"]["argv"][1:] == loaded["engine"]["startup_args"]


def test_config_create_rejects_unsupported_gguf_and_binds_existing_strata_json(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr(installation, "_FILE_CACHE", {})
    monkeypatch.setattr("platform.system", lambda: "Linux")
    monkeypatch.setattr("platform.machine", lambda: "x86_64")
    monkeypatch.setattr(
        "inferyard.config.local_engines.candidate.environment_snapshot",
        lambda: {"ac_online": None},
    )

    ps_root, exe = _create_polystrata_layout(
        tmp_path / "ps-linux",
        platform_kind="linux",
        build_json={"backend": "cuda"},
    )
    inst_path = tmp_path / "inst.json"
    inst_path.write_bytes(json_bytes(build_installation("polystrata", exe)))
    bundle = _write_bundle(tmp_path / "bundle.json")

    # Unsupported architecture (dense llama) is rejected
    bad_gguf = _write_gguf(
        tmp_path / "llama3.gguf",
        {
            "general.name": "Llama-3-8B",
            "general.architecture": "llama",
            "tokenizer.chat_template": "{{ messages }}",
        },
    )
    with pytest.raises(PreparationError, match="polystrata_gguf_architecture_unsupported"):
        create_candidate(
            _candidate_request(
                inst_path=inst_path,
                model_path=bad_gguf,
                bundle_path=bundle,
                out_dir=tmp_path / "cand-bad",
                results_dir=tmp_path / "res-bad",
            )
        )

    # Existing strata-*.json pointing to a pack directory
    pack_dir = tmp_path / "qwen38-pack"
    pack_dir.mkdir()
    (pack_dir / "tokenizer_config.json").write_text(
        json.dumps({"chat_template": "{{ messages }}"}), encoding="utf-8"
    )
    (pack_dir / "weights.bin").write_bytes(b"pack-weights")
    strata_json = ps_root / "data" / "strata-qwen38.json"
    strata_json.write_text(
        json.dumps({"engine": "strata", "pack_dir": str(pack_dir), "context": 65536}),
        encoding="utf-8",
    )

    details = create_candidate(
        _candidate_request(
            inst_path=inst_path,
            model_path=strata_json,
            bundle_path=bundle,
            out_dir=tmp_path / "cand-pack",
            results_dir=tmp_path / "res-pack",
            context_size=None,
        )
    )
    loaded = load_config(details["candidate"]).config.to_dict()
    assert loaded["model"]["kind"] == "directory"
    assert loaded["model"]["local_path"] == str(pack_dir.resolve())
    assert loaded["engine"]["polystrata_config_path"] == str(strata_json.resolve())
    assert loaded["conditions"]["context_size"] == 65536
    assert not (tmp_path / "cand-pack" / "strata-candidate.json").exists()

    # Overriding context_size derives an isolated strata-candidate.json without mutating original
    details_override = create_candidate(
        _candidate_request(
            inst_path=inst_path,
            model_path=strata_json,
            bundle_path=bundle,
            out_dir=tmp_path / "cand-pack-override",
            results_dir=tmp_path / "res-pack-override",
            context_size=131072,
        )
    )
    loaded_override = load_config(details_override["candidate"]).config.to_dict()
    assert loaded_override["conditions"]["context_size"] == 131072
    assert loaded_override["engine"]["polystrata_config_path"] == str(
        (tmp_path / "cand-pack-override" / "strata-candidate.json").resolve()
    )
    assert json.loads(strata_json.read_text(encoding="utf-8"))["context"] == 65536


def test_polystrata_cross_platform_service_binding_and_offline_verify(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr(installation, "_FILE_CACHE", {})
    monkeypatch.setattr("platform.system", lambda: "Linux")
    monkeypatch.setattr("platform.machine", lambda: "x86_64")
    monkeypatch.setattr(
        "inferyard.config.local_engines.candidate.environment_snapshot",
        lambda: {"ac_online": None},
    )

    ps_root, exe = _create_polystrata_layout(
        tmp_path / "ps-linux",
        platform_kind="linux",
        build_json={"backend": "cuda"},
    )
    worker_bin = ps_root / "engine-poly" / "strata-poly"
    inst_path = tmp_path / "inst.json"
    inst_path.write_bytes(json_bytes(build_installation("polystrata", exe)))
    gguf = _write_gguf(
        tmp_path / "gemma4.gguf",
        {
            "general.name": "Gemma-4-26B",
            "general.architecture": "gemma4",
            "tokenizer.chat_template": "{{ messages }}",
        },
    )
    bundle = _write_bundle(tmp_path / "bundle.json")
    out_dir = tmp_path / "cand"
    details = create_candidate(
        _candidate_request(
            inst_path=inst_path,
            model_path=gguf,
            bundle_path=bundle,
            out_dir=out_dir,
            results_dir=tmp_path / "results",
            context_size=32768,
            port=48857,
        )
    )
    config = load_config(details["candidate"]).config.to_dict()
    config["endpoint"]["server_pid"] = 500
    config["endpoint"]["process_start_ticks"] = 9999

    env_vars = dict(config["engine"]["environment"])
    server_cmdline = [str(exe.resolve()), *config["engine"]["startup_args"]]
    worker_cmdline = [str(worker_bin.resolve()), "--worker"]

    class FakeProc:
        def __init__(self, pid: int):
            self.pid = pid

        def ppid(self) -> int:
            return 500 if self.pid == 501 else 1

        def cmdline(self) -> list[str]:
            return server_cmdline if self.pid == 500 else worker_cmdline

        def exe(self) -> str:
            return str(exe.resolve()) if self.pid == 500 else str(worker_bin.resolve())

        def cwd(self) -> str:
            return str(ps_root.resolve())

        def environ(self) -> dict[str, str]:
            return dict(env_vars)

        def net_connections(self, kind: str = "tcp"):
            if self.pid != 500:
                return []
            return [
                SimpleNamespace(
                    status="LISTEN",
                    laddr=SimpleNamespace(ip="127.0.0.1", port=48857),
                )
            ]

        def children(self, recursive: bool = False):
            return [FakeProc(501)] if self.pid == 500 else []

    fake_psutil = SimpleNamespace(Process=FakeProc, Error=OSError)
    monkeypatch.setattr(service.native, "psutil_module", lambda: fake_psutil)
    monkeypatch.setattr(service, "process_start_ticks", lambda pid: 9999 if pid == 500 else 10001)

    binding = service.bind_service(config)
    assert binding["service"]["listener_source"] == "psutil:TCP:LISTEN:pid"
    assert len(binding["service"]["workers"]) == 1
    assert binding["service"]["workers"][0]["pid"] == 501

    binding_path = out_dir / "binding.json"
    binding_bytes = json_bytes(binding)
    binding_path.write_bytes(binding_bytes)
    config["endpoint"]["binding_path"] = str(binding_path.resolve())
    config["endpoint"]["binding_sha256"] = hashlib.sha256(binding_bytes).hexdigest()

    # Verify offline frozen identity check passes cleanly
    runtime_doc = json.loads(Path(config["engine"]["installation_path"]).read_text(encoding="utf-8"))
    assets_doc = json.loads(Path(config["model"]["assets_path"]).read_text(encoding="utf-8"))
    inventory_files = [
        hash_file(Path(config["engine"]["installation_path"])),
        hash_file(binding_path),
        hash_file(Path(config["model"]["assets_path"])),
        hash_file(Path(config["model"]["template_path"])),
        hash_file(gguf),
    ]
    verified_svc = validate_frozen_identity(
        config,
        {"runtime": runtime_doc, "binding": binding, "model_assets": assets_doc},
        files=inventory_files,
    )
    assert verified_svc["pid"] == 500
    assert os.path.samefile(verified_svc["model_binding"]["path"], gguf)
