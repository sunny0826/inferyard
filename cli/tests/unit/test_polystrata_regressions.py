"""Regression tests for PolyStrata preflight, checksum, resume, GPU, and runner guards."""

from __future__ import annotations

import io
import json
import os
from pathlib import Path
import time

import pytest

from inferyard.provisioning import polystrata_prepare
from inferyard.provisioning.polystrata_preflight import GIB, run_polystrata_preflight
from inferyard.provisioning.preparation_io import PreparationError
from inferyard.provisioning.transfer_progress import ProgressEmitter
from polystrata_test_helpers import (
    _HAPPY_SETUP_PY,
    _disk_usage,
    _make_fake_venv,
    _tar_gz_file,
    _zip_file,
)


def test_round1_regressions_sibling_checksum_resume_gpu_build_and_config(tmp_path: Path):
    # 1. Sibling PolyStrata installation with local models/ triggers preflight blocker
    sibling = tmp_path / "PolyStrata-old"
    (sibling / "models").mkdir(parents=True)
    (sibling / "setup.py").write_text("# old\n", encoding="utf-8")
    (sibling / "models" / "weights.gguf").write_bytes(b"data")
    with pytest.raises(PreparationError) as exc_sib:
        run_polystrata_preflight(
            tmp_path / "PolyStrata-new",
            tmp_path / "data-new",
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    assert exc_sib.value.reason == "polystrata_sibling_migration_risk"

    # 2. Local archive with wrong --expected-sha256 fails closed BEFORE running setup.py
    isolated = tmp_path / "nested" / "level" / "isolated"
    isolated.mkdir(parents=True)
    zip_arch = _zip_file(
        isolated / "ps.zip",
        {
            "setup.py": _HAPPY_SETUP_PY.encode("utf-8"),
            "serve/server.py": b"# server\n",
        },
    )
    with pytest.raises(PreparationError) as exc_sha:
        polystrata_prepare.prepare_polystrata(
            isolated / "out-bad-sha",
            data_dir=isolated / "data-bad-sha",
            source=str(zip_arch),
            expected_sha256="0" * 64,
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            venv_creator=_make_fake_venv,
        )
    assert exc_sha.value.reason == "engine_checksum_mismatch"
    assert not (isolated / "out-bad-sha").exists()

    # 3. --resume refuses directory lacking .inferyard-runtime.json receipt
    unverified_dir = isolated / "out-unverified"
    (unverified_dir / "serve").mkdir(parents=True)
    (unverified_dir / "setup.py").write_text(_HAPPY_SETUP_PY, encoding="utf-8")
    (unverified_dir / "serve" / "server.py").write_text("# server\n", encoding="utf-8")
    with pytest.raises(PreparationError) as exc_unv:
        polystrata_prepare.prepare_polystrata(
            unverified_dir,
            data_dir=isolated / "data-unv",
            source=str(zip_arch),
            resume=True,
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            venv_creator=_make_fake_venv,
        )
    assert exc_unv.value.reason == "output_exists"

    # 4. Resume disk credit subtracts existing .part bytes in data_dir/models
    resume_data = isolated / "data-part-credit"
    part_dir = resume_data / "models" / "qwen36-UD-Q4_K_M"
    part_dir.mkdir(parents=True)
    part_file = part_dir / "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf.part"
    with part_file.open("wb") as stream:
        stream.truncate(20 * GIB)
    credit_report = run_polystrata_preflight(
        isolated / "out-part-credit",
        resume_data,
        family="qwen36",
        model="UD-Q4_K_M",
        resume=True,
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(10 * GIB),
    )
    assert credit_report["blockers"] == []

    # 5. Credentialed STRATA_ROCM_INDEX blocked in preflight
    with pytest.raises(PreparationError) as exc_env:
        run_polystrata_preflight(
            isolated / "out-cred",
            isolated / "data-cred",
            platform_info=("Linux", "x64"),
            linux_venv_check=lambda _py: True,
            gpu_probe=lambda _sys: {"status": "available", "devices": [{"vendor": "amd"}]},
            build_tools_check=lambda _nvcc: [],
            disk_usage_fn=_disk_usage(200 * GIB),
            env={"STRATA_ROCM_INDEX": "https://secret_tok@rocm.internal/simple"},
        )
    assert exc_env.value.reason == "polystrata_credentialed_env_url"

    # 6. GPU preflight blocks backend=auto with no GPU, cuda with N/A driver, and missing Linux build tools
    with pytest.raises(PreparationError) as exc_nogpu:
        run_polystrata_preflight(
            isolated / "out-nogpu",
            isolated / "data-nogpu",
            backend="auto",
            platform_info=("Linux", "x64"),
            linux_venv_check=lambda _py: True,
            gpu_probe=lambda _sys: {"status": "unavailable", "devices": []},
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    assert exc_nogpu.value.reason == "polystrata_gpu_missing"

    with pytest.raises(PreparationError) as exc_na:
        run_polystrata_preflight(
            isolated / "out-na",
            isolated / "data-na",
            backend="cuda",
            platform_info=("Linux", "x64"),
            linux_venv_check=lambda _py: True,
            gpu_probe=lambda _sys: {
                "status": "available",
                "devices": [{"vendor": "nvidia", "driver_version": "N/A"}],
            },
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    assert exc_na.value.reason == "polystrata_nvidia_driver_unverified"

    with pytest.raises(PreparationError) as exc_bt:
        run_polystrata_preflight(
            isolated / "out-bt",
            isolated / "data-bt",
            family="qwen36",
            backend="cuda",
            platform_info=("Linux", "x64"),
            linux_venv_check=lambda _py: True,
            gpu_probe=lambda _sys: {
                "status": "available",
                "devices": [{"vendor": "nvidia", "driver_version": "580.65"}],
            },
            build_tools_check=lambda need_nvcc: ["g++", "nvcc"] if need_nvcc else ["g++"],
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    assert exc_bt.value.reason == "polystrata_build_tools_missing"

    # 7. Stage 4 rejects port-only config missing valid exe & list args
    bad_cfg_setup = """\
import json, os
from pathlib import Path
root = Path(__file__).resolve().parent
d = root / "engine-poly"
d.mkdir(exist_ok=True)
b = d / ("strata-poly.exe" if os.name == "nt" else "strata-poly")
b.write_text("#!/bin/sh\\n", encoding="utf-8")
if os.name != "nt":
    os.chmod(b, 0o755)
(root / "strata-bad.json").write_text(json.dumps({"port": 8080}), encoding="utf-8")
"""
    bad_cfg_arch = _tar_gz_file(
        isolated / "bad-cfg.tar.gz",
        [
            ("file", "setup.py", bad_cfg_setup.encode("utf-8")),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
        ],
    )
    with pytest.raises(PreparationError) as exc_badcfg:
        polystrata_prepare.prepare_polystrata(
            isolated / "out-badcfg",
            data_dir=isolated / "data-badcfg",
            source=str(bad_cfg_arch),
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            venv_creator=_make_fake_venv,
        )
    assert exc_badcfg.value.reason == "polystrata_config_missing"

    # 8. Step 4 download line keeps stage=setup_engine instead of flipping to downloading_model
    from inferyard.provisioning.polystrata_runner import parse_setup_line

    buf = io.StringIO()
    em = ProgressEmitter(enabled=True, stream=buf, min_interval_seconds=0.0)
    st, step = parse_setup_line("=== Step 4: the PolyStrata engine ===", "setup_engine", None, em)
    st, step = parse_setup_line("  llama.cpp.zip: 10.0 / 20.0 MB (50%)", st, step, em)
    assert st == "setup_engine"
    parsed_events = [json.loads(line) for line in buf.getvalue().splitlines()]
    assert all(ev["stage"] == "setup_engine" for ev in parsed_events)


def test_round2_regressions_resume_datadir_gguf_mismatch_broken_pipe_and_scoped_credit(tmp_path: Path):
    # 1. Changing --data-dir on --resume when isolated settings.json points to previous data with models
    prev_out = tmp_path / "run-a" / "out"
    prev_data = tmp_path / "run-a" / "old-data"
    (prev_data / "models").mkdir(parents=True)
    (prev_data / "models" / "old.gguf").write_bytes(b"weights")
    cfg_dir = prev_out / ".inferyard-config" / "polystrata"
    cfg_dir.mkdir(parents=True)
    (cfg_dir / "settings.json").write_text(json.dumps({"data_dir": str(prev_data)}), encoding="utf-8")
    with pytest.raises(PreparationError) as exc_dd:
        run_polystrata_preflight(
            prev_out,
            tmp_path / "run-a" / "new-data",
            resume=True,
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    assert exc_dd.value.reason == "polystrata_data_dir_mismatch"

    # 2. macOS explicit --family qwen --model Q2_0 with Qwen3.6 file must be rejected
    gguf_dir = tmp_path / "ggufs"
    gguf_dir.mkdir()
    q36_file = gguf_dir / "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf"
    q36_file.write_bytes(b"GGUF")
    with pytest.raises(PreparationError) as exc_mm:
        run_polystrata_preflight(
            tmp_path / "run-b" / "out",
            tmp_path / "run-b" / "data",
            family="qwen",
            model="Q2_0",
            gguf_path=q36_file,
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    assert exc_mm.value.reason == "polystrata_gguf_unmatched_model"

    # 3. Unrelated 100 GiB file in another model folder must NOT reduce Qwen3.6 disk budget
    shared_data = tmp_path / "run-c" / "data"
    other_dir = shared_data / "models" / "other-model"
    other_dir.mkdir(parents=True)
    with (other_dir / "huge-other.gguf").open("wb") as stream:
        stream.truncate(100 * GIB)
    with pytest.raises(PreparationError) as exc_disk:
        run_polystrata_preflight(
            tmp_path / "run-c" / "out",
            shared_data,
            family="qwen36",
            model="UD-Q4_K_M",
            resume=True,
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(3 * GIB),
        )
    assert exc_disk.value.reason == "polystrata_disk_insufficient"

    # 4. Linux deepseek/kolibri source-build families require build tools in preflight
    for fam in ("deepseek", "kolibri"):
        with pytest.raises(PreparationError) as exc_fam:
            run_polystrata_preflight(
                tmp_path / f"run-{fam}" / "out",
                tmp_path / f"run-{fam}" / "data",
                family=fam,
                backend="cuda",
                platform_info=("Linux", "x64"),
                linux_venv_check=lambda _py: True,
                gpu_probe=lambda _sys: {
                    "status": "available",
                    "devices": [{"vendor": "nvidia", "driver_version": "580.65"}],
                },
                build_tools_check=lambda _nvcc: ["g++"],
                disk_usage_fn=_disk_usage(200 * GIB),
            )
        assert exc_fam.value.reason == "polystrata_build_tools_missing"

    # 5. BrokenPipeError from progress stream still cleans up the setup process group
    if os.name != "nt":
        pid_file = tmp_path / "broken_pipe_child.pid"
        bp_setup = f"""\
import os, sys, time
from pathlib import Path
Path({str(pid_file)!r}).write_text(str(os.getpid()), encoding="utf-8")
print("=== Step 3: Python packages ===", flush=True)
print("=== Step 4: building engine ===", flush=True)
time.sleep(60)
"""
        bp_out = tmp_path / "run-bp" / "out"
        bp_data = tmp_path / "run-bp" / "data"
        bp_out.mkdir(parents=True)
        bp_data.mkdir(parents=True)
        (bp_out / "setup.py").write_text(bp_setup, encoding="utf-8")
        venv_py = _make_fake_venv(bp_out)

        class _BrokenStream:
            def __init__(self):
                self.count = 0

            def write(self, _data):
                self.count += 1
                if self.count >= 2:
                    raise BrokenPipeError("consumer closed")
                return len(_data)

            def flush(self):
                pass

        from inferyard.provisioning.polystrata_runner import run_setup_process

        with pytest.raises(BrokenPipeError):
            run_setup_process(
                bp_out,
                venv_py,
                bp_data,
                system="Darwin",
                family=None,
                model=None,
                context=None,
                backend=None,
                model_source=None,
                gguf_path=None,
                proxy_origin=None,
                timeout_seconds=30,
                emitter=ProgressEmitter(enabled=True, stream=_BrokenStream(), min_interval_seconds=0.0),
            )
        child_pid = int(pid_file.read_text(encoding="utf-8").strip())
        reap_deadline = time.monotonic() + 3.0
        alive = True
        while time.monotonic() < reap_deadline:
            try:
                os.kill(child_pid, 0)
            except ProcessLookupError:
                alive = False
                break
            time.sleep(0.05)
        assert not alive, f"setup pid {child_pid} survived BrokenPipeError"
