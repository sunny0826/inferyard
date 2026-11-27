"""Unit tests for PolyStrata reasoning_effort config and bound worker RSS aggregation."""

from __future__ import annotations

import json
import struct
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.config.loader import load_config
from inferyard.config.local_engines.candidate import create as create_candidate
from inferyard.contracts.validation import json_bytes
from inferyard.platforms.local_engines.detect import build_installation
from inferyard.platforms.telemetry import Sampler, bound_workers, read_rss, sample_memory
from inferyard.provisioning.preparation_io import PreparationError, sha256


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


def _write_bundle(path: Path, name: str = "zh-smoke.json") -> Path:
    from inferyard.bundles.registry import bundle_bytes

    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(bundle_bytes(name.removesuffix(".json")))
    return path


def _setup_polystrata(tmp_path: Path) -> tuple[Path, Path, Path]:
    root = tmp_path / "ps"
    (root / "serve").mkdir(parents=True)
    (root / "serve" / "server.py").write_text("print('server')\n", encoding="utf-8")
    (root / "setup.py").write_text("print('setup')\n", encoding="utf-8")
    venv = root / ".venv"
    venv.mkdir()
    (venv / "pyvenv.cfg").write_text("home = /usr/bin\n", encoding="utf-8")
    exe = venv / "bin" / "python"
    exe.parent.mkdir(parents=True)
    exe.write_bytes(b"#!/bin/sh\n")
    exe.chmod(0o755)
    for rel in ("engine-poly/strata-poly", "engine/strata"):
        binary = root / rel
        binary.parent.mkdir(parents=True, exist_ok=True)
        binary.write_bytes(b"bin")
        binary.chmod(0o755)
    (root / "engine-poly" / "BUILD.json").write_text('{"backend":"metal"}', encoding="utf-8")
    inst = tmp_path / "inst.json"
    inst.write_bytes(json_bytes(build_installation("polystrata", exe)))
    return root, inst, root / "engine" / "strata"


def test_polystrata_reasoning_effort_scales_max_tokens_and_preserves_thinking_keys(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr("platform.system", lambda: "Darwin")
    monkeypatch.setattr("platform.machine", lambda: "arm64")
    monkeypatch.setattr(
        "inferyard.config.local_engines.candidate.environment_snapshot",
        lambda: {"ac_online": True},
    )

    ps_root, inst_path, strata_bin = _setup_polystrata(tmp_path)
    bundle = _write_bundle(tmp_path / "bundle.json")
    gguf = _write_gguf(
        tmp_path / "qwen38.gguf",
        {
            "general.name": "Qwen3.8-Flash-Next",
            "general.architecture": "qwen35moe",
            "tokenizer.chat_template": "{{ messages }}",
        },
    )
    cfg_json = tmp_path / "strata-q2_0.json"
    cfg_json.write_text(
        json.dumps(
            {
                "exe": str(strata_bin),
                "args": ["--native", str(gguf), "--max-context", "32768"],
                "cwd": str(ps_root / "engine"),
                "model_name": "qwen3.8-flash-next-q2_0",
                "reasoning_budget_tokens": 1024,
                "reasoning_close_retry": True,
                "reasoning_loop_recovery": "recover",
            }
        ),
        encoding="utf-8",
    )

    out_low = tmp_path / "cand-low"
    create_candidate(
        SimpleNamespace(
            installation=inst_path,
            model_path=cfg_json,
            bundle_path=bundle,
            out=out_low,
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
            reasoning_effort="low",
        )
    )
    loaded = load_config(out_low / "candidate.toml").config.to_dict()
    assert loaded["generation"]["reasoning_effort"] == "low"
    assert loaded["generation"]["max_tokens"] == 2048
    derived = json.loads((out_low / "strata-candidate.json").read_text(encoding="utf-8"))
    assert derived["reasoning_budget_tokens"] == 1024
    assert derived["reasoning_close_retry"] is True
    assert derived["reasoning_loop_recovery"] == "recover"

    out_capped = tmp_path / "cand-capped"
    create_candidate(
        SimpleNamespace(
            installation=inst_path,
            model_path=cfg_json,
            bundle_path=bundle,
            out=out_capped,
            output_root=tmp_path / "results",
            context_size=1024,
            served_model=None,
            port=48857,
            server_pid=None,
            model_repo=None,
            model_revision=None,
            max_tokens=None,
            timeout_seconds=None,
            prompt_cache=None,
            reasoning_effort="high",
        )
    )
    loaded_capped = load_config(out_capped / "candidate.toml").config.to_dict()
    assert loaded_capped["generation"]["reasoning_effort"] == "high"
    assert loaded_capped["generation"]["max_tokens"] == 1024


def test_bound_workers_rss_aggregation_and_incarnation_guard(tmp_path: Path) -> None:
    proc = tmp_path / "proc"
    proc.mkdir()
    (proc / "meminfo").write_text("MemAvailable: 16777216 kB\n", encoding="utf-8")

    def write_proc(pid: int, ticks: int, rss_kb: int) -> None:
        pdir = proc / str(pid)
        pdir.mkdir(exist_ok=True)
        stat_fields = ["0"] * 50
        stat_fields[19] = str(ticks)
        (pdir / "stat").write_text(f"{pid} (proc) " + " ".join(stat_fields) + "\n", encoding="utf-8")
        (pdir / "status").write_text(f"Name:\tproc\nVmRSS:\t{rss_kb} kB\n", encoding="utf-8")

    write_proc(100, 1000, 96 * 1024)         # server.py (~96 MiB)
    write_proc(101, 1010, 38 * 1024 * 1024)  # strata C++ engine (38 GiB)
    write_proc(102, 1020, 512 * 1024)        # strata-vision (512 MiB)

    binding_doc = {
        "definition": "local-engine-binding.v1",
        "schema_version": 3,
        "service": {
            "pid": 100,
            "start_ticks": 1000,
            "workers": [
                {"pid": 101, "start_ticks": 1010, "parent_pid": 100},
                {"pid": 102, "start_ticks": 1020, "parent_pid": 100},
            ],
        },
    }
    raw_binding = json_bytes(binding_doc)
    binding_path = tmp_path / "binding.json"
    binding_path.write_bytes(raw_binding)
    config = {
        "endpoint": {
            "server_pid": 100,
            "process_start_ticks": 1000,
            "binding_path": str(binding_path),
            "binding_sha256": sha256(raw_binding),
        },
        "telemetry": {"interval_ms": 500},
    }
    workers = bound_workers(config)
    assert workers == ((101, 1010), (102, 1020))

    expected_bytes = (96 + 38 * 1024 + 512) * 1024 * 1024
    val, reason = read_rss(100, 1000, proc, workers=workers)
    assert reason is None
    assert val == expected_bytes

    samples = sample_memory(100, 1000, "formal", "case-1", proc, workers=workers)
    rss_sample = next(s for s in samples if s["metric_name"] == "service_rss")
    assert rss_sample["value"] == expected_bytes
    assert rss_sample["server_pid"] == 100
    assert rss_sample["process_start_ticks"] == 1000
    assert rss_sample["missing_reason"] is None

    sampler = Sampler(SimpleNamespace(), config)
    assert sampler.workers == ((101, 1010), (102, 1020))

    # If a worker restarts (start_ticks changes), read_rss must report source_changed
    write_proc(101, 9999, 38 * 1024 * 1024)
    val_changed, reason_changed = read_rss(100, 1000, proc, workers=workers)
    assert val_changed is None
    assert reason_changed == "source_changed"
