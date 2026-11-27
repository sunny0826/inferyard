"""Unit tests for `inferyard serve prepare` CLI and plan generation."""

from __future__ import annotations

import json
import os
import struct
from pathlib import Path

import jsonschema

from inferyard import SCHEMA_VERSION
from inferyard.cli import main
from inferyard.config.local_engines.launch import startup
from inferyard.contracts.schemas import export_schema
from inferyard.contracts.validation import validate_document
from inferyard.platforms.local_engines.installation import read_installation
from inferyard.provisioning.runtime_profiles import RELEASE, load_profile, output_files


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


def _write_installation(
    root: Path,
    *,
    family: str,
    executable_name: str = "engine-bin",
    environment: dict[str, str] | None = None,
    extra: dict | None = None,
) -> Path:
    root.mkdir(parents=True, exist_ok=True)
    exe = root / executable_name
    exe.write_text("#!/bin/sh\n", encoding="utf-8")
    exe.chmod(0o755)
    payload = {
        "schema_version": SCHEMA_VERSION,
        "definition": "local-engine-installation.v1",
        "family": family,
        "executable": str(exe),
        "working_directory": str(root),
        "runtime_roots": [str(root)],
        "environment": environment or {},
    }
    if extra:
        payload.update(extra)
    inst_file = root / "installation.json"
    inst_file.write_text(json.dumps(payload), encoding="utf-8")
    return inst_file


def _write_valid_receipt(rt_dir: Path, profile_name: str) -> tuple[Path, Path]:
    profile, digest = load_profile(profile_name)
    rt_dir.mkdir(parents=True, exist_ok=True)
    _, manifest_rel, _, expected = output_files(profile["members"])
    manifest_path = rt_dir / manifest_rel
    manifest_path.parent.mkdir(parents=True, exist_ok=True)
    manifest_path.write_text(json.dumps(profile["libraries"]), encoding="utf-8")
    for relative, member in expected.items():
        item = rt_dir / relative
        item.parent.mkdir(parents=True, exist_ok=True)
        with item.open("wb") as stream:
            os.truncate(stream.fileno(), member["bytes"])
    exe = rt_dir / profile["engine_binary"]
    receipt_payload = {
        "kind": "community_runtime_receipt.v1",
        "schema_version": SCHEMA_VERSION,
        "profile": profile["profile"],
        "profile_sha256": digest,
        "platform": profile["platform"],
        "architecture": profile["architecture"],
        "mode": profile["mode"],
        "engine_release": profile["engine_release"],
        "archives": profile["archives"],
        "engine_binary": profile["engine_binary"],
        "runtime_library_manifest": profile["runtime_library_manifest"],
        "engine_sha256": profile["libraries"]["llama-server.exe"],
        "libraries": profile["libraries"],
        "version": {"argv": ["--version"], "returncode": 0, "stdout": "ok", "stderr": ""},
        "model_requests_sent": 0,
        "ready_to_run": False,
    }
    receipt = rt_dir / "runtime-receipt.json"
    receipt.write_text(json.dumps(receipt_payload), encoding="utf-8")
    return receipt, exe


def _run_cli(capsys, argv: list[str]) -> tuple[int, dict]:
    code = main(argv)
    captured = capsys.readouterr()
    return code, json.loads(captured.out)


def _assert_valid_serve_plan(plan: dict) -> None:
    jsonschema.validate(plan, export_schema("serve_plan"))
    validate_document("serve_plan", plan)


def test_serve_prepare_community_engine_and_overwrite(capsys, tmp_path):
    model = _write_gguf(
        tmp_path / "qwen.gguf",
        {
            "general.architecture": "qwen2",
            "general.name": "Qwen Test",
            "qwen2.context_length": 32768,
        },
    )
    engine = tmp_path / "bin" / "llama-server"
    engine.parent.mkdir(parents=True)
    engine.write_text("#!/bin/sh\n", encoding="utf-8")
    engine.chmod(0o755)
    out = tmp_path / "serve-out"

    code, payload = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(model), "--engine", str(engine),
            "--context-size", "12000", "--port", "49001", "--out", str(out), "--json",
        ],
    )
    assert code == 0
    assert payload["status"] == "prepared"
    plan_path = out / "serve-plan.json"
    assert plan_path.is_file()
    plan = json.loads(plan_path.read_text(encoding="utf-8"))
    _assert_valid_serve_plan(plan)
    assert plan["local"] is False
    assert plan["family"] == "llama-cpp"
    assert plan["adapter"] == "prism_llama_server_v1"
    assert plan["model_kind"] == "gguf"
    assert plan["context_size"] == 12000
    assert plan["endpoint"] == "http://127.0.0.1:49001"
    assert "-c" in plan["argv"] and "12000" in plan["argv"]
    assert "--host" in plan["argv"] and "127.0.0.1" in plan["argv"]
    assert "-np" not in plan["argv"]
    assert "--no-cache-prompt" not in plan["argv"]
    assert "--no-cache-idle-slots" not in plan["argv"]
    assert "--cache-ram" not in plan["argv"]

    code2, _ = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(model), "--engine", str(engine),
            "--context-size", "32768", "--port", "49002", "--out", str(out), "--json",
        ],
    )
    assert code2 == 0
    plan2 = json.loads(plan_path.read_text(encoding="utf-8"))
    _assert_valid_serve_plan(plan2)
    assert plan2["endpoint"] == "http://127.0.0.1:49002"
    assert plan2["context_size"] == 32768


def test_serve_prepare_runtime_receipt_validation(capsys, tmp_path):
    model = _write_gguf(
        tmp_path / "model.gguf",
        {"general.architecture": "llama", "llama.context_length": 16384},
    )
    receipt, exe = _write_valid_receipt(tmp_path / "runtime", f"{RELEASE}-win-cuda-12.4-x64")
    out = tmp_path / "out"

    code, _ = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(model), "--runtime-receipt", str(receipt),
            "--context-size", "8192", "--port", "49010", "--out", str(out), "--json",
        ],
    )
    assert code == 0
    plan = json.loads((out / "serve-plan.json").read_text(encoding="utf-8"))
    _assert_valid_serve_plan(plan)
    assert plan["local"] is False
    assert plan["argv"][0] == str(exe.resolve())
    assert "-ngl" in plan["argv"] and "999" in plan["argv"]

    # Removing a required DLL fails with runtime_asset_mismatch.
    profile, _ = load_profile(f"{RELEASE}-win-cuda-12.4-x64")
    _, manifest_rel, _, expected = output_files(profile["members"])
    first_dll = next(rel for rel in expected if rel.endswith(".dll"))
    (tmp_path / "runtime" / first_dll).unlink()
    code_missing, res_missing = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(model), "--runtime-receipt", str(receipt),
            "--context-size", "8192", "--out", str(tmp_path / "out-missing-dll"),
        ],
    )
    assert code_missing == 2
    assert res_missing["limitations"] == ["runtime_asset_mismatch"]

    # Truncated library manifest also fails with runtime_asset_mismatch.
    (tmp_path / "runtime" / manifest_rel).write_text("{truncated", encoding="utf-8")
    code_trunc, res_trunc = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(model), "--runtime-receipt", str(receipt),
            "--context-size", "8192", "--out", str(tmp_path / "out-trunc-manifest"),
        ],
    )
    assert code_trunc == 2
    assert res_trunc["limitations"] == ["runtime_asset_mismatch"]

    # Rejects incomplete 2-field receipt.
    bad_dir = tmp_path / "bad-runtime"
    bad_dir.mkdir()
    (bad_dir / "llama-server.exe").write_bytes(b"MZ")
    bad_receipt = bad_dir / "runtime-receipt.json"
    bad_receipt.write_text(
        json.dumps({"engine_binary": "llama-server.exe", "mode": "cuda"}), encoding="utf-8"
    )
    code_bad, res_bad = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(model), "--runtime-receipt", str(bad_receipt),
            "--context-size", "8192", "--out", str(tmp_path / "out-bad-receipt"),
        ],
    )
    assert code_bad == 2
    assert res_bad["limitations"] == ["runtime_receipt_invalid"]


def test_serve_prepare_installation_dir_and_llama_cpp(capsys, tmp_path):
    model = _write_gguf(
        tmp_path / "m.gguf",
        {"general.architecture": "llama", "llama.context_length": 16384},
    )
    inst_dir = tmp_path / "inst-llama"
    _write_installation(inst_dir, family="llama-cpp", executable_name="llama-server")
    out = tmp_path / "out"

    code, _ = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(model), "--installation", str(inst_dir),
            "--context-size", "8192", "--port", "49020", "--out", str(out), "--json",
        ],
    )
    assert code == 0
    plan = json.loads((out / "serve-plan.json").read_text(encoding="utf-8"))
    _assert_valid_serve_plan(plan)
    assert plan["local"] is True
    assert plan["family"] == "llama-cpp"
    assert plan["adapter"] == "local_openai_v1"
    assert "-np" not in plan["argv"]
    assert "--no-context-shift" not in plan["argv"]
    assert "--no-cache-prompt" not in plan["argv"]


def test_serve_prepare_sglang_vllm_and_unsupported_families(capsys, tmp_path):
    model_dir = tmp_path / "hf-model"
    model_dir.mkdir()
    (model_dir / "config.json").write_text(
        json.dumps({"max_position_embeddings": 65536}), encoding="utf-8"
    )

    sg_inst = _write_installation(tmp_path / "inst-sg", family="sglang", executable_name="python")
    sg_out = tmp_path / "out-sg"
    code, _ = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(model_dir), "--installation", str(sg_inst),
            "--context-size", "16384", "--port", "49030", "--out", str(sg_out), "--json",
        ],
    )
    assert code == 0
    sg_plan = json.loads((sg_out / "serve-plan.json").read_text(encoding="utf-8"))
    _assert_valid_serve_plan(sg_plan)
    assert sg_plan["family"] == "sglang"
    assert sg_plan["model_kind"] == "directory"
    assert "--max-running-requests" not in sg_plan["argv"]
    assert "--context-length" in sg_plan["argv"] and "16384" in sg_plan["argv"]
    assert Path(sg_plan["working_directory"]).is_dir()
    assert Path(sg_plan["environment"]["PROMETHEUS_MULTIPROC_DIR"]).is_dir()
    assert sg_plan["environment"]["SGLANG_RUN_ID"].startswith("inferyard-serve-")

    vllm_inst = _write_installation(tmp_path / "inst-vllm", family="vllm", executable_name="python")
    vllm_out = tmp_path / "out-vllm"
    code, _ = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(model_dir), "--installation", str(vllm_inst),
            "--context-size", "8192", "--port", "49031", "--out", str(vllm_out),
        ],
    )
    assert code == 0
    vllm_plan = json.loads((vllm_out / "serve-plan.json").read_text(encoding="utf-8"))
    _assert_valid_serve_plan(vllm_plan)
    assert "--max-num-seqs" not in vllm_plan["argv"]
    assert "--max-model-len" in vllm_plan["argv"] and "8192" in vllm_plan["argv"]

    # mlx-lm: standalone plan launches upstream mlx_lm.server without unsupported flags.
    mlx_inst = _write_installation(tmp_path / "inst-mlx", family="mlx-lm", executable_name="python")
    mlx_out = tmp_path / "out-mlx"
    code_mlx, res_mlx = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(model_dir), "--installation", str(mlx_inst),
            "--context-size", "8192", "--port", "49032", "--out", str(mlx_out), "--json",
        ],
    )
    assert code_mlx == 0
    assert res_mlx["status"] == "prepared"
    mlx_plan = json.loads((mlx_out / "serve-plan.json").read_text(encoding="utf-8"))
    _assert_valid_serve_plan(mlx_plan)
    assert mlx_plan["family"] == "mlx-lm"
    assert mlx_plan["local"] is True
    assert mlx_plan["model_kind"] == "directory"
    assert mlx_plan["argv"][1:5] == ["-m", "mlx_lm.server", "--model", str(model_dir.resolve())]
    assert "--served-model-name" not in mlx_plan["argv"]
    assert "--max-model-len" not in mlx_plan["argv"]
    bench_args, _ = startup(
        read_installation(mlx_inst), model_dir, "hf-model", 8192, 49032, standalone=False
    )
    assert "--max-model-len" in bench_args and "--served-model-name" in bench_args

    # lmstudio: rejected.
    models_root = tmp_path / "lms-models"
    models_root.mkdir()
    lms_inst = _write_installation(
        tmp_path / "inst-lms",
        family="lmstudio",
        executable_name="lms",
        extra={"models_root": str(models_root)},
    )
    gguf = _write_gguf(
        tmp_path / "lms.gguf",
        {"general.architecture": "llama", "llama.context_length": 8192},
    )
    code_lms, lms_res = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(gguf), "--installation", str(lms_inst),
            "--context-size", "4096", "--out", str(tmp_path / "out-lms"),
        ],
    )
    assert code_lms == 2
    assert lms_res["limitations"] == ["serve_family_unsupported"]


def test_serve_prepare_directory_context_limits(capsys, tmp_path):
    vllm_inst = _write_installation(tmp_path / "inst-vllm", family="vllm", executable_name="python")

    nested_dir = tmp_path / "nested-model"
    nested_dir.mkdir()
    (nested_dir / "config.json").write_text(
        json.dumps({"text_config": {"max_position_embeddings": 4096}}), encoding="utf-8"
    )
    code, res = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(nested_dir), "--installation", str(vllm_inst),
            "--context-size", "65536", "--out", str(tmp_path / "out-nested-over"),
        ],
    )
    assert code == 2
    assert res["limitations"] == ["model_context_unsupported"]

    code_ok, _ = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(nested_dir), "--installation", str(vllm_inst),
            "--context-size", "4096", "--out", str(tmp_path / "out-nested-ok"),
        ],
    )
    assert code_ok == 0

    missing_cfg = tmp_path / "missing-cfg-model"
    missing_cfg.mkdir()
    code_miss, res_miss = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(missing_cfg), "--installation", str(vllm_inst),
            "--context-size", "4096", "--out", str(tmp_path / "out-miss"),
        ],
    )
    assert code_miss == 2
    assert res_miss["limitations"] == ["model_context_unsupported"]


def test_serve_prepare_rejects_context_overflow_and_bad_args(capsys, tmp_path):
    model = _write_gguf(
        tmp_path / "small.gguf",
        {"general.architecture": "llama", "llama.context_length": 4096},
    )
    engine = tmp_path / "llama-server"
    engine.write_text("#!/bin/sh\n", encoding="utf-8")
    engine.chmod(0o755)

    code, res = _run_cli(
        capsys,
        [
            "serve", "prepare", "--model", str(model), "--engine", str(engine),
            "--context-size", "8192", "--out", str(tmp_path / "out-over"),
        ],
    )
    assert code == 2
    assert res["limitations"] == ["model_context_unsupported"]

    for bad_args in (
        ["--context-size", "0"],
        ["--context-size", "-10"],
        ["--context-size", "2048", "--port", "0"],
        ["--context-size", "2048", "--port", "70000"],
    ):
        code, res = _run_cli(
            capsys,
            [
                "serve", "prepare", "--model", str(model), "--engine", str(engine),
                *bad_args, "--out", str(tmp_path / "out-bad"),
            ],
        )
        assert code == 2
        assert res["limitations"] == ["invalid_input"]
