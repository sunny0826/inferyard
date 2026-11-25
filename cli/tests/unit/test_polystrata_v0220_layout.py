"""Regression tests for PolyStrata 0.2.20+ (terminal/ & engine/build-metal) layout."""

from __future__ import annotations

import json
import os
from pathlib import Path
import struct
from types import SimpleNamespace

import pytest

from inferyard.config.loader import load_config
from inferyard.config.local_engines.candidate import create as create_candidate
from inferyard.platforms.local_engines import installation
from inferyard.platforms.local_engines.detect import build_installation, detect_local_engines
from inferyard.platforms.local_engines.detect_polystrata import (
    collect_polystrata_targets,
    infer_polystrata_root,
)
from inferyard.platforms.local_engines.service import _polystrata_worker_env_policy
from inferyard.platforms.local_engines.startup import validate_arguments
from inferyard.provisioning import polystrata_prepare
from inferyard.provisioning.polystrata_preflight import GIB
from inferyard.provisioning.polystrata_preflight_checks import _estimate_model_disk_bytes
from polystrata_test_helpers import (
    _disk_usage,
    _make_fake_venv,
    _tar_gz_file,
    _write_fake_python,
    _write_fake_uv,
)

_V0220_SETUP_PY = """\
import json, os, struct, sys
from pathlib import Path

term_dir = Path(__file__).resolve().parent
root = term_dir.parent
argv = sys.argv[1:]
(root / "received_argv.json").write_text(json.dumps(argv), encoding="utf-8")

build_dir = root / "engine" / "build-metal"
build_dir.mkdir(parents=True, exist_ok=True)
binary = build_dir / ("strata.exe" if os.name == "nt" else "strata-metal")
binary.write_text("#!/bin/sh\\necho strata-metal\\n", encoding="utf-8")
if os.name != "nt":
    os.chmod(binary, 0o755)
(build_dir / "BUILD-poly.json").write_text(
    json.dumps({"version": "0.2.20", "backend": "metal"}), encoding="utf-8"
)

vision_dir = root / "engine" / "build-vision" / "bin"
vision_dir.mkdir(parents=True, exist_ok=True)
vision_bin = vision_dir / ("strata-vision.exe" if os.name == "nt" else "strata-vision")
vision_bin.write_text("#!/bin/sh\\necho strata-vision\\n", encoding="utf-8")
if os.name != "nt":
    os.chmod(vision_bin, 0o755)

data_dir = Path(argv[argv.index("--data-dir") + 1])
pack_dir = data_dir / "packs" / "q2_0"
pack_dir.mkdir(parents=True, exist_ok=True)
(pack_dir / "dense.bin").write_bytes(b"dense-weights")

model_dir = data_dir / "models" / "Q2_0"
model_dir.mkdir(parents=True, exist_ok=True)
shard = model_dir / "Qwen3.8-Flash-Next-Q2_0-00001-of-00002.gguf"
kvs = [
    (b"general.architecture", b"qwen35moe"),
    (b"tokenizer.chat_template", b"{{ messages[0].content }}"),
]
gguf_parts = [struct.pack("<4sIQQ", b"GGUF", 3, 1, len(kvs))]
for key_b, val_b in kvs:
    gguf_parts.append(struct.pack("<Q", len(key_b)) + key_b)
    gguf_parts.append(struct.pack("<IQ", 8, len(val_b)) + val_b)
gguf_parts.append(b"tensors")
shard.write_bytes(b"".join(gguf_parts))

(term_dir / "strata-q2_0.json").write_text(
    json.dumps(
        {
            "exe": str(binary),
            "args": [
                "--pack", str(pack_dir),
                "--native", str(shard),
                "--mmproj-bin", str(vision_bin),
                "--max-context", "32768",
            ],
            "cwd": str(term_dir),
            "model_name": "qwen3.8-flash-next-q2_0",
            "port": 8080,
        }
    ),
    encoding="utf-8",
)
"""


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


def test_prepare_polystrata_v0220_terminal_layout_end_to_end(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr(installation, "_FILE_CACHE", {})

    fake_bin = tmp_path / "fake-bin"
    _write_fake_python(fake_bin, "python3.12", 3, 12)
    _write_fake_uv(fake_bin)
    env = {"PATH": f"{fake_bin}{os.pathsep}{os.environ.get('PATH', '')}"}

    prefix = "PolyStrata-d62dfacbe185dfbb447e79c5fb1091ae189afc64"
    archive = _tar_gz_file(
        tmp_path / "polystrata-v0220.tar.gz",
        [
            ("dir", f"{prefix}/terminal"),
            ("dir", f"{prefix}/terminal/serve"),
            ("dir", f"{prefix}/terminal/strata_cli"),
            ("dir", f"{prefix}/engine"),
            ("file", f"{prefix}/terminal/setup.py", _V0220_SETUP_PY.encode("utf-8")),
            ("file", f"{prefix}/terminal/polystrata.py", b"APP = 'polystrata'\n"),
            ("file", f"{prefix}/terminal/serve/server.py", b"print('v0220 server')\n"),
            ("file", f"{prefix}/terminal/strata_cli/__init__.py", b"APP_NAME = 'PolyStrata'\n"),
            ("file", f"{prefix}/engine/CMakeLists.txt", b"cmake_minimum_required(VERSION 3.20)\n"),
            ("file", f"{prefix}/engine/dummy.cpp", b"int main() { return 0; }\n"),
        ],
    )
    out_dir = tmp_path / "engines" / "polystrata-v0220"
    out_dir.parent.mkdir(parents=True, exist_ok=True)
    data_dir = tmp_path / "models" / "polystrata-data"

    result = polystrata_prepare.prepare_polystrata(
        out_dir,
        data_dir=data_dir,
        family="qwen",
        model="Q2_0",
        context=32768,
        backend="metal",
        source=str(archive),
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(200 * GIB),
        venv_creator=_make_fake_venv,
        env=env,
    )

    assert result["ready_to_run"] is True
    assert result["version"] == "0.2.20"
    assert result["backend"] == "metal"
    cfg_path = Path(result["model_config_path"])
    assert cfg_path == (out_dir / "terminal" / "strata-q2_0.json").resolve()

    runtime_roots = result["installation"]["runtime_roots"]
    assert str((out_dir / "engine").resolve()) not in runtime_roots
    assert str((out_dir / "engine" / "build-metal").resolve()) in runtime_roots
    assert str((out_dir / "engine" / "build-vision").resolve()) in runtime_roots
    assert str((out_dir / "terminal" / "serve").resolve()) in runtime_roots
    assert str((out_dir / "terminal" / "strata_cli").resolve()) in runtime_roots

    inst_path = Path(result["installation_path"])
    snapshot = installation.freeze_installation(result["installation"])
    runtime_files = installation.verify_installation(snapshot)
    assert snapshot["installation"]["family"] == "polystrata"
    assert any(
        item.path.endswith("strata-metal") or item.path.endswith("strata.exe")
        for item in runtime_files
    )

    # Candidate creation & startup argument validation must accept terminal/serve/server.py
    # and terminal/strata-q2_0.json with cwd=<out_dir>/terminal and build-vision/bin/strata-vision.
    if os.name != "nt":
        monkeypatch.setattr("platform.system", lambda: "Darwin")
        monkeypatch.setattr("platform.machine", lambda: "arm64")
        monkeypatch.setattr(
            "inferyard.config.local_engines.candidate.environment_snapshot",
            lambda: {"ac_online": True},
        )
        bundle_path = _write_bundle(tmp_path / "zh-smoke.json")
        req = SimpleNamespace(
            installation=inst_path,
            model_path=cfg_path,
            bundle_path=bundle_path,
            out=tmp_path / "cand-v0220",
            output_root=tmp_path / "results",
            context_size=16384,
            served_model="qwen38-q2",
            port=48857,
            server_pid=None,
            model_repo=None,
            model_revision=None,
            max_tokens=256,
            timeout_seconds=600,
            prompt_cache=None,
            temperature_stop_override_reason=None,
        )
        created = create_candidate(req)
        loaded = load_config(created["candidate"]).config.to_dict()
        startup_args = loaded["engine"]["startup_args"]
        server_py = (out_dir / "terminal" / "serve" / "server.py").resolve()
        assert Path(startup_args[0]).resolve() == server_py
        validate_arguments(loaded)

        allowed_env, _libs, _expected = _polystrata_worker_env_policy(loaded)
        assert "OMP_NUM_THREADS" in allowed_env


def test_preflight_shared_data_dir_deducts_existing_q2_0_shards_without_resume(
    tmp_path: Path,
) -> None:
    empty_data = tmp_path / "empty-data"
    shared_data = tmp_path / "shared-data"
    shard_dir = shared_data / "models" / "Q2_0"
    shard_dir.mkdir(parents=True)
    shard_bytes = 8 * 1024 * 1024
    # Filename intentionally omits 'q2_0' to ensure tag directory models/Q2_0 suffices.
    shard = shard_dir / "Qwen-00001-of-00002.gguf"
    shard.write_bytes(b"x" * shard_bytes)

    # Unrelated Swift-Qwen3.8 Q2_0 shard in shared_data must NOT be deducted for qwen/Q2_0.
    swift_dir = shared_data / "models" / "swift-Q2_0"
    swift_dir.mkdir(parents=True)
    swift_shard = swift_dir / "Swift-Qwen3.8-Flash-Next-GSQ-RCO-Q2_0-00001-of-00002.gguf"
    swift_shard.write_bytes(b"s" * (16 * 1024 * 1024))

    fresh_req, exact_fresh = _estimate_model_disk_bytes(
        system="Darwin",
        family="qwen",
        model="Q2_0",
        gguf_path=None,
        resume=False,
        data_dir=empty_data,
    )
    shared_req, exact_shared = _estimate_model_disk_bytes(
        system="Darwin",
        family="qwen",
        model="Q2_0",
        gguf_path=None,
        resume=False,
        data_dir=shared_data,
    )
    assert exact_fresh is True and exact_shared is True
    assert shared_req == fresh_req - shard_bytes

    # Gemma 4 draft_bytes must also be deductible when both main + draft GGUF exist.
    gemma_dir = shared_data / "models" / "gemma4-UD-Q3_K_M"
    (gemma_dir / "MTP").mkdir(parents=True)
    main_bytes, draft_bytes = 6 * 1024 * 1024, 2 * 1024 * 1024
    (gemma_dir / "gemma-4-26B-A4B-it-UD-Q3_K_M.gguf").write_bytes(b"m" * main_bytes)
    (gemma_dir / "MTP" / "mtp-gemma-4-26B-A4B-it-Q8_0.gguf").write_bytes(b"d" * draft_bytes)
    gemma_fresh, _ = _estimate_model_disk_bytes(
        system="Linux",
        backend="cuda",
        family="gemma4",
        model="UD-Q3_K_M",
        gguf_path=None,
        resume=False,
        data_dir=empty_data,
    )
    gemma_shared, _ = _estimate_model_disk_bytes(
        system="Linux",
        backend="cuda",
        family="gemma4",
        model="UD-Q3_K_M",
        gguf_path=None,
        resume=False,
        data_dir=shared_data,
    )
    assert gemma_shared == gemma_fresh - (main_bytes + draft_bytes)


def test_detect_and_target_discovery_for_v0220_layout(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "empty-home"))
    root = tmp_path / "ps-v0220"
    (root / "terminal" / "serve").mkdir(parents=True)
    (root / "terminal" / "setup.py").write_text("print('setup')\n", encoding="utf-8")
    (root / "terminal" / "serve" / "server.py").write_text("print('srv')\n", encoding="utf-8")
    build_dir = root / "engine" / "build-metal"
    build_dir.mkdir(parents=True)
    bin_name = "strata.exe" if os.name == "nt" else "strata-metal"
    binary = build_dir / bin_name
    binary.write_text("#!/bin/sh\n", encoding="utf-8")
    if os.name != "nt":
        binary.chmod(0o755)
    (build_dir / "BUILD-poly.json").write_text(
        json.dumps({"version": "0.2.20", "backend": "metal"}), encoding="utf-8"
    )
    (root / "terminal" / "strata-q2_0.json").write_text("{}", encoding="utf-8")

    # Also place a Linux/Windows-style terminal/engine binary so both root and root/terminal
    # would validate if not normalized; passing terminal/ or terminal/setup.py must normalize
    # to root without emitting duplicate candidates.
    term_eng_dir = root / "terminal" / "engine"
    term_eng_dir.mkdir(parents=True)
    term_bin = term_eng_dir / ("strata.exe" if os.name == "nt" else "strata")
    term_bin.write_text("#!/bin/sh\n", encoding="utf-8")
    if os.name != "nt":
        term_bin.chmod(0o755)

    # Venv inside terminal/.venv (manual upstream install) as well as root/.venv
    term_venv_py = _make_fake_venv(root / "terminal")
    assert infer_polystrata_root(term_venv_py) == root.resolve()

    targets = [
        p.resolve()
        for p, _ in collect_polystrata_targets(
            (
                str(root / "terminal"),
                str(root / "terminal" / "setup.py"),
                str(root / "terminal" / "serve" / "server.py"),
            ),
            tmp_path / "empty-home",
            tmp_path / "empty-cwd",
        )
    ]
    assert {p for p in targets if p.is_relative_to(root.resolve())} == {root.resolve()}

    detected = detect_local_engines(
        extra_paths=(str(root / "terminal"), str(root / "terminal" / "setup.py")),
    )
    poly_hits = [
        item
        for item in detected["candidates"]
        if item["family"] == "polystrata"
        and Path(item["working_directory"]).is_relative_to(root.resolve())
    ]
    assert len(poly_hits) == 1
    assert poly_hits[0]["working_directory"] == str(root.resolve())
    assert poly_hits[0]["version"] == "0.2.20"
    assert str((root / "terminal" / "strata-q2_0.json").resolve()) in poly_hits[0]["model_configs"]

    built = build_installation("polystrata", term_venv_py, working_directory=root / "terminal")
    assert built["working_directory"] == str(root.resolve())
