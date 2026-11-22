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
from inferyard.config.local_engines.polystrata import select_polystrata_backend
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
from inferyard.platforms.local_engines import assets as local_assets
from inferyard.platforms.local_engines import installation, service
from inferyard.platforms.local_engines.detect import build_installation, detect_local_engines
from inferyard.platforms.local_engines.offline import validate_frozen_identity
from inferyard.platforms.local_engines.startup import validate_arguments
from inferyard.provisioning.preparation_io import PreparationError
from inferyard.runtime.local_profile import safety_policy
from local_engine_helpers import local_config  # noqa: F401


def _write_gguf(path: Path, metadata: dict[str, str | int], payload: bytes = b"tensors") -> Path:
    parts = [struct.pack("<4sIQQ", b"GGUF", 3, 1, len(metadata))]
    for key, value in metadata.items():
        key_bytes = key.encode("utf-8")
        parts.append(struct.pack("<Q", len(key_bytes)) + key_bytes)
        if isinstance(value, str):
            val_bytes = value.encode("utf-8")
            parts.append(struct.pack("<IQ", 8, len(val_bytes)) + val_bytes)
        else:
            parts.append(struct.pack("<II", 4, value))
    parts.append(payload)
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
    build_in_engine_dir: bool = True,
    with_data_dir: bool = True,
) -> tuple[Path, Path, Path]:
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
        engine_bin = root / "build-metal" / "strata-poly"
    exe.parent.mkdir(parents=True, exist_ok=True)
    exe.write_bytes(b"#!/bin/sh\n")
    exe.chmod(0o755)
    engine_bin.parent.mkdir(parents=True, exist_ok=True)
    engine_bin.write_bytes(b"synthetic-engine-binary")
    engine_bin.chmod(0o755)

    if build_json is not None:
        build_target = (engine_bin.parent if build_in_engine_dir else root) / "BUILD.json"
        build_target.write_text(json.dumps(build_json), encoding="utf-8")
    if with_data_dir:
        (root / "data").mkdir(exist_ok=True)
    return root, exe, engine_bin


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


def test_polystrata_profiles_hip_schema_and_legacy_macos_constraints(local_config) -> None:
    assert "polystrata" in LOCAL_FAMILIES
    assert "polystrata" in DIAGNOSTIC_ENGINES
    assert {"qwen35moe", "glm5-next", "glm5next", "deepseek4"} <= set(
        POLYSTRATA_SUPPORTED_GGUF_ARCHS
    )

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

    cfg = deepcopy(local_config)
    cfg["model"]["kind"] = "gguf"
    cfg["model"]["local_path"] = "/models/qwen36.gguf"
    cfg["engine"].update(
        family="polystrata",
        backend="hip",
        working_directory="/opt/polystrata",
        polystrata_config_path="/out/strata-candidate.json",
        polystrata_config_sha256="a" * 64,
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

    # Historical macOS families MUST remain metal + require_temperature=True
    legacy_vllm = deepcopy(local_config)
    legacy_vllm["engine"]["family"] = "vllm"
    legacy_vllm["engine"]["backend"] = "cpu"
    legacy_vllm["safety"]["require_temperature"] = False
    with pytest.raises(ContractError, match="backend"):
        validate_config(legacy_vllm)

    # Metal backend still enforces require_temperature=True even for polystrata
    metal_bad = deepcopy(cfg)
    metal_bad["engine"]["backend"] = "metal"
    metal_bad["safety"]["require_temperature"] = False
    with pytest.raises(ContractError, match="require_temperature"):
        validate_config(metal_bad)

    # safety_policy still blocks null max_temperature_celsius without override reason
    plan = {
        "experiment": {
            "safety": {
                "interval_seconds": 0.5,
                "max_temperature_celsius": None,
                "require_temperature": False,
                "check_environment": True,
            },
            "workloads": [{"protocol": {"kind": "fixed"}}],
        }
    }
    with pytest.raises(PreflightError, match="local_engine_safety_policy_required"):
        safety_policy(plan, cfg)
    plan["experiment"]["safety"]["max_temperature_celsius"] = 85
    assert safety_policy(plan, cfg)["require_temperature"] is False

    # server.py outside working_directory is rejected
    wrong_server = deepcopy(cfg)
    wrong_server["engine"]["startup_args"][0] = "/elsewhere/serve/server.py"
    with pytest.raises(PreflightError, match="local_engine_entrypoint_mismatch"):
        validate_arguments(wrong_server)

    # --open is rejected
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
    monkeypatch.setattr("platform.system", lambda: "Darwin")

    ws_ps, ws_exe, _ = _create_polystrata_layout(
        tmp_path / "engines" / "polystrata",
        platform_kind="macos",
        build_json={"version": "v0.1.45-poly.10", "backend": "metal"},
    )
    (ws_ps / "data" / "strata-qwen36.json").write_text("{}", encoding="utf-8")

    # Empty engine dir or non-executable python must NOT be detected
    empty_ps, empty_exe, empty_bin = _create_polystrata_layout(
        tmp_path / "engines" / "polystrata-empty", platform_kind="macos"
    )
    empty_bin.unlink()
    non_exec_ps, non_exec_exe, _ = _create_polystrata_layout(
        tmp_path / "engines" / "polystrata-noexec", platform_kind="macos"
    )
    non_exec_exe.chmod(0o644)

    result = detect_local_engines()
    ps_candidates = [c for c in result["candidates"] if c["family"] == "polystrata"]
    assert len(ps_candidates) == 1

    ws_entry = ps_candidates[0]
    assert ws_entry["working_directory"] == str(ws_ps.resolve())
    assert ws_entry["executable"] == str(ws_exe.resolve())
    assert ws_entry["version"] == "v0.1.45-poly.10"
    assert ws_entry["data_dir"] == str((ws_ps / "data").resolve())
    assert ws_entry["model_configs"] == [str((ws_ps / "data" / "strata-qwen36.json").resolve())]
    assert str((ws_ps / "data").resolve()) not in ws_entry["runtime_roots"]
    assert str((ws_ps / "serve").resolve()) in ws_entry["runtime_roots"]
    assert str((ws_ps / "build-metal").resolve()) in ws_entry["runtime_roots"]


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

    ps_root, exe, engine_bin = _create_polystrata_layout(
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

    # Removing engine binary while leaving directory empty must fail engine check
    engine_bin.unlink()
    bad_code, bad_res = execute_engine_check(
        SimpleNamespace(command="engine check", installation=out_json)
    )
    assert bad_code == 0
    assert bad_res.details["eligible"] is False
    assert bad_res.details["reason"] == "local_engine_polystrata_engine_missing"


def test_select_polystrata_backend_fallback_and_hip_gpu_probe(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr("platform.system", lambda: "Linux")
    ps_root, exe, _ = _create_polystrata_layout(
        tmp_path / "ps-nobuild", platform_kind="linux", build_json=None
    )
    decl = build_installation("polystrata", exe)

    monkeypatch.setattr(
        "inferyard.platforms.device_gpu.detect_gpus",
        lambda system: {
            "status": "observed",
            "source": "rocm-smi",
            "devices": [{"vendor": "amd", "name": "RX 7900 XTX"}],
        },
    )
    assert select_polystrata_backend(decl, "Linux") == "hip"

    # Also verify BUILD.json inside engine-poly/ takes precedence
    (ps_root / "engine-poly" / "BUILD.json").write_text(
        json.dumps({"backend": "cuda"}), encoding="utf-8"
    )
    assert select_polystrata_backend(decl, "Linux") == "cuda"


@pytest.mark.parametrize(
    ("platform_kind", "sys_name", "machine", "expected_backend", "expected_temp"),
    [
        ("macos", "Darwin", "arm64", "metal", True),
        ("linux", "Linux", "x86_64", "hip", False),
        ("windows", "Windows", "AMD64", "cuda", False),
    ],
)
def test_config_create_shared_gguf_upstream_contract_and_describe(
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

    ps_root, exe, engine_bin = _create_polystrata_layout(
        tmp_path / f"ps-{platform_kind}",
        platform_kind=platform_kind,
        build_json={"version": "v0.1.45-poly.10", "backend": expected_backend},
    )
    tok_dir = ps_root / "data" / "packs" / "glm5" / "tokenizer"
    tok_dir.mkdir(parents=True)
    (tok_dir / "vocab.json").write_text("{}", encoding="utf-8")
    (ps_root / "tools").mkdir(exist_ok=True)
    (ps_root / "tools" / "strata_tokenizer.py").write_text("# tokenizer\n", encoding="utf-8")

    inst_path = tmp_path / f"inst-{platform_kind}.json"
    inst_path.write_bytes(json_bytes(build_installation("polystrata", exe)))

    gguf = _write_gguf(
        tmp_path / "models" / "glm5next.gguf",
        {
            "general.name": "GLM-5-Next",
            "general.architecture": "glm5next",
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
    derived_cfg = Path(details["polystrata_config"])
    assert derived_cfg == out_dir.resolve() / "strata-candidate.json"
    derived_data = json.loads(derived_cfg.read_text(encoding="utf-8"))

    # Matches upstream serve/server.py engine_exe(), engine_args(), tokenizer, and model_name
    assert derived_data["exe"] == str(engine_bin.resolve())
    assert derived_data["args"] == ["--model", str(gguf.resolve()), "--max-context", "65536"]
    assert derived_data["cwd"] == str(ps_root.resolve())
    assert derived_data["tokenizer"] == str(tok_dir.resolve())
    assert derived_data["model_name"] == "qwen36-local"
    assert derived_data["port"] == 48857
    assert list(out_dir.glob("*.gguf")) == []

    # tools/strata_tokenizer.py is frozen inside the runtime snapshot
    runtime_snap = json.loads(
        Path(details["installation"]).read_text(encoding="utf-8")
    )
    frozen_root_paths = {r["path"] for r in runtime_snap["roots"]}
    assert str((ps_root / "tools" / "strata_tokenizer.py").resolve()) in frozen_root_paths

    loaded = load_config(details["candidate"]).config.to_dict()
    assert loaded["engine"]["backend"] == expected_backend
    assert loaded["engine"]["polystrata_config_sha256"] == hash_file(derived_cfg).sha256
    assert loaded["safety"]["require_temperature"] is expected_temp
    assert loaded["generation"]["reasoning_effort"] == "none"

    described = describe_candidate(SimpleNamespace(candidate=Path(details["candidate"])))
    assert described["family"] == "polystrata"
    assert described["launch"]["argv"][0] == str(exe.resolve())

    # Tampering with strata-candidate.json after creation is detected by assets()
    tampered = dict(derived_data)
    tampered["args"] = ["--model", str(gguf.resolve()), "--max-context", "1024"]
    derived_cfg.write_text(json.dumps(tampered), encoding="utf-8")
    with pytest.raises(PreflightError, match="identity_hash_mismatch"):
        local_assets.assets(loaded)


def test_upstream_pack_config_credential_rejection_and_arch_enforcement(
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

    ps_root, exe, engine_bin = _create_polystrata_layout(
        tmp_path / "ps-linux",
        platform_kind="linux",
        build_json={"backend": "cuda"},
    )
    inst_path = tmp_path / "inst.json"
    inst_path.write_bytes(json_bytes(build_installation("polystrata", exe)))
    bundle = _write_bundle(tmp_path / "bundle.json")

    # Unsupported architecture (llama) rejected BOTH directly AND via existing JSON with poly exe
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
                out_dir=tmp_path / "cand-bad-direct",
                results_dir=tmp_path / "res-bad-direct",
            )
        )
    bad_json = ps_root / "data" / "strata-llama.json"
    bad_json.write_text(
        json.dumps(
            {
                "exe": str(engine_bin),
                "args": ["--model", str(bad_gguf), "--max-context", "8192"],
                "cwd": str(ps_root),
            }
        ),
        encoding="utf-8",
    )
    with pytest.raises(PreparationError, match="polystrata_gguf_architecture_unsupported"):
        create_candidate(
            _candidate_request(
                inst_path=inst_path,
                model_path=bad_json,
                bundle_path=bundle,
                out_dir=tmp_path / "cand-bad-json",
                results_dir=tmp_path / "res-bad-json",
            )
        )

    # Plaintext api_key in existing JSON is rejected (never copied to candidate output)
    good_gguf = _write_gguf(
        tmp_path / "qwen36.gguf",
        {
            "general.name": "Qwen3.6",
            "general.architecture": "qwen35moe",
            "tokenizer.chat_template": "{{ messages }}",
        },
    )
    secret_json = ps_root / "data" / "strata-secret.json"
    secret_json.write_text(
        json.dumps(
            {
                "exe": str(engine_bin),
                "args": ["--model", str(good_gguf), "--max-context", "32768"],
                "cwd": str(ps_root),
                "api_key": "synthetic-secret-token",
            }
        ),
        encoding="utf-8",
    )
    with pytest.raises(PreparationError, match="polystrata_plaintext_credential_rejected"):
        create_candidate(
            _candidate_request(
                inst_path=inst_path,
                model_path=secret_json,
                bundle_path=bundle,
                out_dir=tmp_path / "cand-secret",
                results_dir=tmp_path / "res-secret",
                context_size=65536,
            )
        )

    # Real upstream --pack + chat_template.jinja + default ctx 4096
    pack_dir = tmp_path / "packs" / "iq2_xs"
    tok_dir = pack_dir / "tokenizer"
    tok_dir.mkdir(parents=True)
    (tok_dir / "vocab.json").write_text("{}", encoding="utf-8")
    (tok_dir / "chat_template.jinja").write_text("{{ messages }}", encoding="utf-8")
    (pack_dir / "weights.bin").write_bytes(b"pack-weights")
    upstream_pack_json = ps_root / "data" / "strata-iq2_xs.json"
    upstream_pack_json.write_text(
        json.dumps(
            {
                "exe": str(ps_root / "engine" / "strata"),
                "args": ["--pack", str(pack_dir)],
                "cwd": str(ps_root),
                "tokenizer": str(tok_dir),
                "model_name": "qwen3.8-flash-next-iq2_xs",
            }
        ),
        encoding="utf-8",
    )
    details = create_candidate(
        _candidate_request(
            inst_path=inst_path,
            model_path=upstream_pack_json,
            bundle_path=bundle,
            out_dir=tmp_path / "cand-pack",
            results_dir=tmp_path / "res-pack",
            context_size=None,
        )
    )
    loaded = load_config(details["candidate"]).config.to_dict()
    assert loaded["model"]["kind"] == "directory"
    assert loaded["model"]["local_path"] == str(pack_dir.resolve())
    assert loaded["conditions"]["context_size"] == 4096
    manifest_files = {
        row["path"]
        for row in json.loads(Path(loaded["model"]["assets_path"]).read_text(encoding="utf-8"))[
            "files"
        ]
    }
    assert "weights.bin" in manifest_files

    # mcp_servers credential in existing JSON is rejected
    mcp_secret_json = ps_root / "data" / "strata-mcp-secret.json"
    mcp_secret_json.write_text(
        json.dumps(
            {
                "exe": str(engine_bin),
                "args": ["--model", str(good_gguf), "--max-context", "32768"],
                "cwd": str(ps_root),
                "mcp_servers": {"remote": {"headers": {"Authorization": "Bearer secret"}}},
            }
        ),
        encoding="utf-8",
    )
    with pytest.raises(PreparationError, match="polystrata_plaintext_credential_rejected"):
        create_candidate(
            _candidate_request(
                inst_path=inst_path,
                model_path=mcp_secret_json,
                bundle_path=bundle,
                out_dir=tmp_path / "cand-mcp-secret",
                results_dir=tmp_path / "res-mcp-secret",
                context_size=65536,
            )
        )

    # Relative path inside existing JSON resolves against cwd BEFORE config_file.parent
    cwd_gguf = _write_gguf(
        ps_root / "shared.gguf",
        {
            "general.name": "CwdModel",
            "general.architecture": "qwen35moe",
            "tokenizer.chat_template": "{{ messages }}",
        },
        payload=b"cwd-copy",
    )
    _write_gguf(
        ps_root / "data" / "shared.gguf",
        {
            "general.name": "DataModel",
            "general.architecture": "qwen35moe",
            "tokenizer.chat_template": "{{ messages }}",
        },
        payload=b"data-copy",
    )
    rel_json = ps_root / "data" / "strata-rel.json"
    rel_json.write_text(
        json.dumps(
            {
                "exe": str(engine_bin),
                "args": ["--model", "shared.gguf", "--max-context", "16384"],
                "cwd": str(ps_root),
                "model_name": "qwen36-local",
            }
        ),
        encoding="utf-8",
    )
    rel_details = create_candidate(
        _candidate_request(
            inst_path=inst_path,
            model_path=rel_json,
            bundle_path=bundle,
            out_dir=tmp_path / "cand-rel",
            results_dir=tmp_path / "res-rel",
            context_size=None,
        )
    )
    assert Path(rel_details["model"]) == cwd_gguf.resolve()

    details_override = create_candidate(
        _candidate_request(
            inst_path=inst_path,
            model_path=upstream_pack_json,
            bundle_path=bundle,
            out_dir=tmp_path / "cand-pack-override",
            results_dir=tmp_path / "res-pack-override",
            context_size=65536,
        )
    )
    override_cfg = json.loads(
        Path(details_override["polystrata_config"]).read_text(encoding="utf-8")
    )
    assert "--max-context" in override_cfg["args"]
    idx = override_cfg["args"].index("--max-context")
    assert override_cfg["args"][idx + 1] == "65536"


def test_multishard_gguf_worker_ld_library_path_and_offline_verify(
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

    ps_root, exe, worker_bin = _create_polystrata_layout(
        tmp_path / "ps-linux",
        platform_kind="linux",
        build_json={"backend": "cuda"},
    )
    cuda_lib = tmp_path / "cuda-lib"
    cuda_lib.mkdir()
    inst_path = tmp_path / "inst.json"
    inst_path.write_bytes(json_bytes(build_installation("polystrata", exe)))

    # 2-shard GGUF (like deepseek4 / Qwen3.6)
    shard1 = _write_gguf(
        tmp_path / "models" / "deepseek4-00001-of-00002.gguf",
        {
            "general.name": "DeepSeek-4",
            "general.architecture": "deepseek4",
            "tokenizer.chat_template": "{{ messages }}",
            "split.count": 2,
            "split.no": 0,
        },
        payload=b"shard-one-bytes",
    )
    shard2 = _write_gguf(
        tmp_path / "models" / "deepseek4-00002-of-00002.gguf",
        {
            "general.name": "DeepSeek-4",
            "general.architecture": "deepseek4",
            "split.count": 2,
            "split.no": 1,
        },
        payload=b"shard-two-bytes",
    )
    strata_json = ps_root / "data" / "strata-deepseek4.json"
    strata_json.write_text(
        json.dumps(
            {
                "exe": str(worker_bin),
                "args": ["--model", str(shard1), "--max-context", "32768"],
                "cwd": str(ps_root),
                "lib_dirs": [str(cuda_lib)],
                "gpu": 0,
            }
        ),
        encoding="utf-8",
    )
    bundle = _write_bundle(tmp_path / "bundle.json")
    out_dir = tmp_path / "cand"
    details = create_candidate(
        _candidate_request(
            inst_path=inst_path,
            model_path=strata_json,
            bundle_path=bundle,
            out_dir=out_dir,
            results_dir=tmp_path / "results",
            context_size=None,
            port=48857,
        )
    )
    config = load_config(details["candidate"]).config.to_dict()
    config["endpoint"]["server_pid"] = 500
    config["endpoint"]["process_start_ticks"] = 9999

    env_vars = dict(config["engine"]["environment"])
    worker_env = {
        **env_vars,
        "LD_LIBRARY_PATH": str(cuda_lib),
        "CUDA_VISIBLE_DEVICES": "0",
        "CUDA_DEVICE_ORDER": "PCI_BUS_ID",
    }
    server_cmdline = [str(exe.resolve()), *config["engine"]["startup_args"]]
    worker_cmdline = [str(worker_bin.resolve()), "--model", str(shard1)]
    my_uid = os.getuid() if hasattr(os, "getuid") else 0

    class FakeProc:
        def __init__(self, pid: int):
            self.pid = pid

        def uids(self):
            return SimpleNamespace(real=my_uid)

        def ppid(self) -> int:
            return 500 if self.pid == 501 else 1

        def cmdline(self) -> list[str]:
            return server_cmdline if self.pid == 500 else worker_cmdline

        def exe(self) -> str:
            return str(exe.resolve()) if self.pid == 500 else str(worker_bin.resolve())

        def cwd(self) -> str:
            return str(ps_root.resolve())

        def environ(self) -> dict[str, str]:
            return dict(env_vars) if self.pid == 500 else dict(worker_env)

        def children(self, recursive: bool = False):
            return [FakeProc(501)] if self.pid == 500 else []

    fake_conns = [
        SimpleNamespace(
            status="LISTEN",
            pid=500,
            laddr=SimpleNamespace(ip="127.0.0.1", port=48857),
        )
    ]
    fake_psutil = SimpleNamespace(
        Process=FakeProc,
        Error=OSError,
        net_connections=lambda kind="tcp": list(fake_conns),
    )
    monkeypatch.setattr(service.native, "psutil_module", lambda: fake_psutil)
    monkeypatch.setattr(service, "process_start_ticks", lambda pid: 9999 if pid == 500 else 10001)

    binding = service.bind_service(config)
    assert binding["service"]["listener_source"] == "psutil:TCP:LISTEN:pid"
    assert len(binding["service"]["workers"]) == 1

    # Unauthorized LD_LIBRARY_PATH is still rejected
    worker_env["LD_LIBRARY_PATH"] = "/tmp/untrusted-lib"
    with pytest.raises(PreflightError, match="local_engine_environment_override"):
        service.bind_service(config)
    worker_env["LD_LIBRARY_PATH"] = str(cuda_lib)

    # Ambiguous second listener on same port is rejected
    fake_conns.append(
        SimpleNamespace(
            status="LISTEN",
            pid=999,
            laddr=SimpleNamespace(ip="0.0.0.0", port=48857),
        )
    )
    with pytest.raises(PreflightError, match="local_engine_listener_mismatch"):
        service.bind_service(config)
    fake_conns.pop()

    # Offline verification checks both shards + frozen polystrata_config_sha256
    binding_path = out_dir / "binding.json"
    binding_bytes = json_bytes(binding)
    binding_path.write_bytes(binding_bytes)
    config["endpoint"]["binding_path"] = str(binding_path.resolve())
    config["endpoint"]["binding_sha256"] = hashlib.sha256(binding_bytes).hexdigest()

    local_id, bound_files = local_assets.preflight_assets(config)
    shard_paths = {item.path for item in bound_files}
    assert str(shard1.resolve()) in shard_paths
    assert str(shard2.resolve()) in shard_paths
    assert config["engine"]["polystrata_config_path"] in shard_paths
    assert (
        json.loads(Path(config["engine"]["polystrata_config_path"]).read_text(encoding="utf-8"))[
            "model_name"
        ]
        == "qwen36-local"
    )

    verified_svc = validate_frozen_identity(
        config,
        local_id["local_engine"],
        files=bound_files,
    )
    assert verified_svc["pid"] == 500
