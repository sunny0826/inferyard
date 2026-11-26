"""Unit tests for `inferyard serve prepare` with PolyStrata installations."""

from __future__ import annotations

import json
import sys
from pathlib import Path

from inferyard.config import serve_plan as serve_plan_mod
from tests.unit.test_serve_prepare import (
    _assert_valid_serve_plan,
    _run_cli,
    _write_gguf,
    _write_installation,
)


def _make_polystrata_installation(root: Path) -> Path:
    (root / "serve").mkdir(parents=True)
    (root / "serve" / "server.py").write_text("# server\n", encoding="utf-8")
    (root / "engine-poly").mkdir(parents=True)
    (root / "engine").mkdir(parents=True)
    suffix = ".exe" if sys.platform == "win32" else ""
    for rel in (f"engine-poly/strata-poly{suffix}", f"engine/strata{suffix}"):
        binary = root / rel
        binary.write_text("#!/bin/sh\n", encoding="utf-8")
        binary.chmod(0o755)
    return _write_installation(root, family="polystrata", executable_name="python")


def test_serve_prepare_polystrata_gguf_and_atomic_reprepare(capsys, monkeypatch, tmp_path):
    ps_root = tmp_path / "polystrata"
    inst = _make_polystrata_installation(ps_root)

    model = _write_gguf(
        tmp_path / "qwen35.gguf",
        {
            "general.architecture": "qwen35moe",
            "general.name": "Qwen35MoE",
            "qwen35moe.context_length": 65536,
        },
    )
    out = tmp_path / "out-ps"
    code, _ = _run_cli(
        capsys,
        [
            "serve",
            "prepare",
            "--model",
            str(model),
            "--installation",
            str(inst),
            "--context-size",
            "12000",
            "--port",
            "49040",
            "--out",
            str(out),
            "--json",
        ],
    )
    assert code == 0
    plan1 = json.loads((out / "serve-plan.json").read_text(encoding="utf-8"))
    _assert_valid_serve_plan(plan1)
    assert plan1["family"] == "polystrata"
    assert plan1["local"] is True
    cfg1_path = Path(plan1["argv"][plan1["argv"].index("--config") + 1])
    assert cfg1_path.is_file()
    cfg1 = json.loads(cfg1_path.read_text(encoding="utf-8"))
    assert cfg1["port"] == 49040
    assert cfg1["log"] == str((out / "strata-engine.log").resolve())
    assert "--max-context" in cfg1["args"] and "12000" in cfg1["args"]

    # If publishing serve-plan.json fails on re-prepare, prior plan and config stay intact.
    real_write = serve_plan_mod._write_atomic_json

    def fail_plan_write(path: Path, payload: dict) -> None:
        if path.name == "serve-plan.json":
            raise OSError("simulated disk error")
        real_write(path, payload)

    monkeypatch.setattr(serve_plan_mod, "_write_atomic_json", fail_plan_write)
    code_fail, _ = _run_cli(
        capsys,
        [
            "serve",
            "prepare",
            "--model",
            str(model),
            "--installation",
            str(inst),
            "--context-size",
            "12000",
            "--port",
            "49041",
            "--out",
            str(out),
            "--json",
        ],
    )
    assert code_fail == 4
    still_plan1 = json.loads((out / "serve-plan.json").read_text(encoding="utf-8"))
    assert still_plan1["endpoint"] == "http://127.0.0.1:49040"
    assert json.loads(cfg1_path.read_text(encoding="utf-8"))["port"] == 49040

    # Successful re-prepare publishes new content-addressed config without deleting prior files.
    monkeypatch.setattr(serve_plan_mod, "_write_atomic_json", real_write)
    code_ok, _ = _run_cli(
        capsys,
        [
            "serve",
            "prepare",
            "--model",
            str(model),
            "--installation",
            str(inst),
            "--context-size",
            "12000",
            "--port",
            "49041",
            "--out",
            str(out),
            "--json",
        ],
    )
    assert code_ok == 0
    plan2 = json.loads((out / "serve-plan.json").read_text(encoding="utf-8"))
    cfg2_path = Path(plan2["argv"][plan2["argv"].index("--config") + 1])
    assert cfg2_path.is_file()
    assert cfg2_path != cfg1_path
    assert cfg1_path.is_file()
    assert json.loads(cfg2_path.read_text(encoding="utf-8"))["port"] == 49041


def test_serve_prepare_polystrata_native_pack_context(capsys, tmp_path):
    ps_root = tmp_path / "polystrata"
    inst = _make_polystrata_installation(ps_root)
    suffix = ".exe" if sys.platform == "win32" else ""
    strata_exe = ps_root / "engine" / f"strata{suffix}"

    # Pack directory without HuggingFace config.json, declaring --max-context 32768.
    pack_dir = tmp_path / "pack-with-limit"
    weights_dir = pack_dir / "weights"
    weights_dir.mkdir(parents=True)
    (weights_dir / "model.bin").write_bytes(b"weights")
    (pack_dir / "strata-pack.json").write_text(
        json.dumps(
            {
                "exe": str(strata_exe),
                "args": ["--pack", str(weights_dir), "--max-context", "32768"],
                "cwd": str(ps_root),
                "model_name": "PackModel",
            }
        ),
        encoding="utf-8",
    )

    code_ok, _ = _run_cli(
        capsys,
        [
            "serve",
            "prepare",
            "--model",
            str(pack_dir),
            "--installation",
            str(inst),
            "--context-size",
            "4096",
            "--port",
            "49042",
            "--out",
            str(tmp_path / "out-pack-ok"),
        ],
    )
    assert code_ok == 0
    plan = json.loads((tmp_path / "out-pack-ok" / "serve-plan.json").read_text(encoding="utf-8"))
    _assert_valid_serve_plan(plan)
    assert plan["model_kind"] == "strata_pack"

    # Same-installation config with only managed log path differing must derive while keeping parallel=4.
    parallel_cfg = ps_root / "strata-parallel.json"
    parallel_cfg.write_text(
        json.dumps(
            {
                "exe": str(strata_exe),
                "args": ["--pack", str(weights_dir), "--max-context", "32768"],
                "cwd": str(ps_root),
                "model_name": "PackParallel",
                "parallel": 4,
                "log": str(ps_root / "strata-parallel.log"),
            }
        ),
        encoding="utf-8",
    )
    out_parallel = tmp_path / "out-parallel"
    code_parallel, _ = _run_cli(
        capsys,
        [
            "serve",
            "prepare",
            "--model",
            str(parallel_cfg),
            "--installation",
            str(inst),
            "--context-size",
            "32768",
            "--port",
            "49043",
            "--out",
            str(out_parallel),
        ],
    )
    assert code_parallel == 0
    parallel_plan = json.loads((out_parallel / "serve-plan.json").read_text(encoding="utf-8"))
    parallel_derived_path = Path(parallel_plan["argv"][parallel_plan["argv"].index("--config") + 1])
    assert parallel_derived_path != parallel_cfg.resolve()
    parallel_derived = json.loads(parallel_derived_path.read_text(encoding="utf-8"))
    assert parallel_derived["parallel"] == 4
    assert parallel_derived["log"] == str((out_parallel / "strata-engine.log").resolve())

    code_over, res_over = _run_cli(
        capsys,
        [
            "serve",
            "prepare",
            "--model",
            str(pack_dir),
            "--installation",
            str(inst),
            "--context-size",
            "65536",
            "--out",
            str(tmp_path / "out-pack-over"),
        ],
    )
    assert code_over == 2
    assert res_over["limitations"] == ["model_context_unsupported"]

    # Pack without context declaration is allowed.
    pack_nolimit = tmp_path / "pack-no-limit"
    w2 = pack_nolimit / "weights"
    w2.mkdir(parents=True)
    (w2 / "model.bin").write_bytes(b"weights")
    (pack_nolimit / "strata-pack.json").write_text(
        json.dumps(
            {
                "exe": str(strata_exe),
                "args": ["--pack", str(w2)],
                "cwd": str(ps_root),
                "model_name": "PackNoLimit",
            }
        ),
        encoding="utf-8",
    )
    code_nolimit, _ = _run_cli(
        capsys,
        [
            "serve",
            "prepare",
            "--model",
            str(pack_nolimit),
            "--installation",
            str(inst),
            "--context-size",
            "16384",
            "--out",
            str(tmp_path / "out-pack-nolimit"),
        ],
    )
    assert code_nolimit == 0

    # Pack declaring 131072 while model metadata declares 4096 still enforces 4096.
    pack_both = tmp_path / "pack-both-limits"
    w3 = pack_both / "weights"
    w3.mkdir(parents=True)
    (w3 / "config.json").write_text(
        json.dumps({"max_position_embeddings": 4096}), encoding="utf-8"
    )
    (pack_both / "strata-pack.json").write_text(
        json.dumps(
            {
                "exe": str(strata_exe),
                "args": ["--pack", str(w3), "--max-context", "131072"],
                "cwd": str(ps_root),
                "model_name": "PackBoth",
            }
        ),
        encoding="utf-8",
    )
    code_both, res_both = _run_cli(
        capsys,
        [
            "serve",
            "prepare",
            "--model",
            str(pack_both),
            "--installation",
            str(inst),
            "--context-size",
            "65536",
            "--out",
            str(tmp_path / "out-pack-both"),
        ],
    )
    assert code_both == 2
    assert res_both["limitations"] == ["model_context_unsupported"]

    # Non-GGUF --native file still enforces declared --max-context from strata-*.json.
    pack_native = tmp_path / "pack-native-file"
    pack_native.mkdir(parents=True)
    native_file = pack_native / "weights.bin"
    native_file.write_bytes(b"native")
    (pack_native / "strata-native.json").write_text(
        json.dumps(
            {
                "exe": str(strata_exe),
                "args": ["--native", str(native_file), "--max-context", "4096"],
                "cwd": str(ps_root),
                "model_name": "PackNative",
            }
        ),
        encoding="utf-8",
    )
    code_native, res_native = _run_cli(
        capsys,
        [
            "serve",
            "prepare",
            "--model",
            str(pack_native),
            "--installation",
            str(inst),
            "--context-size",
            "8192",
            "--out",
            str(tmp_path / "out-pack-native"),
        ],
    )
    assert code_native == 2
    assert res_native["limitations"] == ["model_context_unsupported"]
