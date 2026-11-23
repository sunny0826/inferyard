"""Unit tests for PolyStrata preflight blockers, log sanitization, and progress emitter."""

from __future__ import annotations

import io
import json
from pathlib import Path

import pytest

from inferyard.provisioning.polystrata_preflight import GIB, run_polystrata_preflight, sanitize_log_line
from inferyard.provisioning.preparation_io import PreparationError
from inferyard.provisioning.transfer_progress import ProgressEmitter
from polystrata_test_helpers import _disk_usage, _write_fake_python


def test_progress_emitter_backward_compat_and_optional_fields():
    buf = io.StringIO()
    emitter = ProgressEmitter(enabled=True, stream=buf, min_interval_seconds=0.0)
    emitter.update("downloading", 500, 1000)
    emitter.update(
        "setup_engine",
        0,
        None,
        step=4,
        step_total=7,
        percent=62.5,
        detail="  building\nstrata-poly  ",
    )
    lines = [json.loads(line) for line in buf.getvalue().splitlines()]
    assert set(lines[0]) == {
        "event",
        "stage",
        "downloaded_bytes",
        "total_bytes",
        "bytes_per_second",
    }
    assert lines[1]["stage"] == "setup_engine"
    assert lines[1]["step"] == 4
    assert lines[1]["step_total"] == 7
    assert lines[1]["percent"] == 62.5
    assert lines[1]["detail"] == "building strata-poly"


def test_sanitize_log_line_redacts_secrets_and_ansi():
    raw = (
        "\x1b[32mOK\x1b[0m https://alice:s3cret@host/x https://opaque_token_xyz@rocm.internal/whl "
        "Bearer tok123456 hf_1234567890ab --api-key mykey"
    )
    cleaned = sanitize_log_line(raw)
    assert "\x1b" not in cleaned
    assert "s3cret" not in cleaned
    assert "opaque_token_xyz" not in cleaned
    assert "tok123456" not in cleaned
    assert "hf_1234567890ab" not in cleaned
    assert "mykey" not in cleaned
    assert "[REDACTED]" in cleaned


def test_preflight_blockers_are_read_only(tmp_path: Path):
    out = tmp_path / "out-dir"
    data = tmp_path / "data-dir"

    # Unsupported platform
    with pytest.raises(PreparationError) as exc_info:
        run_polystrata_preflight(
            out, data, platform_info=("Linux", "arm64"), disk_usage_fn=_disk_usage(200 * GIB)
        )
    assert exc_info.value.reason == "polystrata_platform_unsupported"
    assert not out.exists() and not data.exists()

    # macOS < 15 and missing Xcode CLT
    with pytest.raises(PreparationError) as exc_info:
        run_polystrata_preflight(
            out,
            data,
            platform_info=("Darwin", "arm64"),
            macos_version="14.7.1",
            xcode_clt_check=lambda: False,
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    codes = [b["code"] for b in exc_info.value.details["blockers"]]
    assert "polystrata_macos_too_old" in codes
    assert "polystrata_xcode_clt_missing" in codes
    clt_blocker = next(
        b for b in exc_info.value.details["blockers"] if b["code"] == "polystrata_xcode_clt_missing"
    )
    assert clt_blocker["fix_command"] == "xcode-select --install"

    # Linux missing python3-venv + old NVIDIA driver (isolated PATH without uv)
    linux_fake_bin = tmp_path / "linux-fake-bin"
    _write_fake_python(linux_fake_bin, "python3.12", 3, 12, venv_ok=False)
    with pytest.raises(PreparationError) as exc_info:
        run_polystrata_preflight(
            out,
            data,
            platform_info=("Linux", "x64"),
            linux_venv_check=lambda _py: False,
            gpu_probe=lambda _sys: {
                "status": "available",
                "devices": [{"vendor": "nvidia", "driver_version": "550.54.14"}],
            },
            disk_usage_fn=_disk_usage(200 * GIB),
            env={"PATH": str(linux_fake_bin)},
        )
    codes = [b["code"] for b in exc_info.value.details["blockers"]]
    assert "polystrata_python_venv_missing" in codes
    assert "polystrata_nvidia_driver_too_old" in codes
    venv_blocker = next(
        b for b in exc_info.value.details["blockers"] if b["code"] == "polystrata_python_venv_missing"
    )
    assert "python3.12-venv" in venv_blocker["fix_command"]

    # Insufficient disk space
    with pytest.raises(PreparationError) as exc_info:
        run_polystrata_preflight(
            out,
            data,
            family="qwen36",
            model="UD-Q4_K_M",
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(5 * GIB),
        )
    assert exc_info.value.reason == "polystrata_disk_insufficient"
    assert not out.exists() and not data.exists()

    # Backend mismatch
    with pytest.raises(PreparationError) as exc_info:
        run_polystrata_preflight(
            out,
            data,
            backend="cuda",
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    assert exc_info.value.reason == "polystrata_backend_unsupported"

    # Missing GGUF path
    with pytest.raises(PreparationError) as exc_info:
        run_polystrata_preflight(
            out,
            data,
            gguf_path=tmp_path / "missing.gguf",
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    assert exc_info.value.reason == "polystrata_gguf_path_missing"


def test_preflight_family_without_model_uses_smallest_size_in_family(tmp_path: Path):
    # qwen36 UD-Q2_K_XL needs ~14 GiB + 2 GiB runtime = ~16 GiB, whereas UD-Q4_K_M needs ~26 GiB.
    # With 20 GiB free and only --family qwen36 (no --model), preflight must NOT falsely block.
    report = run_polystrata_preflight(
        tmp_path / "out-fam",
        tmp_path / "data-fam",
        family="qwen36",
        model=None,
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(20 * GIB),
    )
    assert report["blockers"] == []
