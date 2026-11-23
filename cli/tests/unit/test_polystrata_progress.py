"""Unit tests for monotonic 4-stage PolyStrata setup progress emission."""

from __future__ import annotations

import hashlib
import io
import json
import os
from pathlib import Path
import sys

from inferyard.provisioning import polystrata_prepare
from inferyard.provisioning.polystrata_preflight import GIB
from inferyard.provisioning.polystrata_runner import parse_setup_line
from inferyard.provisioning.transfer_progress import ProgressEmitter
from polystrata_test_helpers import (
    _HAPPY_SETUP_PY,
    _FakeClient,
    _FakeResponse,
    _disk_usage,
    _make_fake_venv,
    _tar_gz_file,
)


def _assert_monotonic_four_steps(events: list[dict]) -> None:
    assert events, "expected at least one progress event"
    steps = [ev["step"] for ev in events if "step" in ev]
    assert steps, "expected step fields on progress events"
    for ev in events:
        if "step_total" in ev:
            assert ev["step_total"] == 4, f"unexpected step_total in {ev}"
        if "step" in ev:
            assert 1 <= ev["step"] <= 4, f"out-of-range step in {ev}"
    for prev, cur in zip(steps, steps[1:]):
        assert cur >= prev, f"step jumped backwards from {prev} to {cur}: {events}"


def test_prepare_polystrata_remote_fresh_and_resume_emit_monotonic_four_steps(tmp_path: Path):
    archive_path = _tar_gz_file(
        tmp_path / "PolyStrata-custom.tar.gz",
        [
            ("dir", "PolyStrata-0.1.45-poly.10"),
            (
                "file",
                "PolyStrata-0.1.45-poly.10/setup.py",
                _HAPPY_SETUP_PY.encode("utf-8"),
            ),
            ("dir", "PolyStrata-0.1.45-poly.10/serve"),
            ("file", "PolyStrata-0.1.45-poly.10/serve/server.py", b"# server\n"),
        ],
    )
    payload = archive_path.read_bytes()
    sha256_hex = hashlib.sha256(payload).hexdigest()
    source_url = "https://github.com/simplexai-labs/PolyStrata/releases/download/v0.1.45-poly.10/PolyStrata-custom.tar.gz"

    # 1. Fresh remote setup
    fresh_client = _FakeClient(lambda _m, _u, _h: _FakeResponse(payload))
    fresh_buf = io.StringIO()
    fresh_out = tmp_path / "ps-fresh"
    polystrata_prepare.prepare_polystrata(
        fresh_out,
        data_dir=tmp_path / "data-fresh",
        family="qwen36",
        model="UD-Q4_K_M",
        source=source_url,
        expected_sha256=sha256_hex,
        client=fresh_client,
        emitter=ProgressEmitter(enabled=True, stream=fresh_buf, min_interval_seconds=0.0),
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(200 * GIB),
        venv_creator=_make_fake_venv,
    )
    fresh_events = [json.loads(line) for line in fresh_buf.getvalue().splitlines()]
    _assert_monotonic_four_steps(fresh_events)
    runtime_dl = [ev for ev in fresh_events if ev["stage"] == "downloading_runtime"]
    assert runtime_dl and all(ev.get("step") == 2 and ev.get("step_total") == 4 for ev in runtime_dl)
    assert any(
        ev["stage"] == "setup_engine"
        and ev.get("step") == 3
        and ev.get("detail") == "creating Python environment"
        for ev in fresh_events
    )

    # 2. Resumed partial runtime transfer -> reusing Python environment
    resume_out = tmp_path / "ps-resume"
    resume_out.mkdir(parents=True)
    split = len(payload) // 2
    (resume_out / "PolyStrata-custom.tar.gz.part").write_bytes(payload[:split])

    def _range_handler(_m: str, _u: str, headers: dict):
        if headers.get("Range") == f"bytes={split}-":
            return _FakeResponse(
                payload[split:],
                status_code=206,
                headers={
                    "Content-Range": f"bytes {split}-{len(payload) - 1}/{len(payload)}",
                    "Content-Length": str(len(payload) - split),
                },
            )
        return _FakeResponse(payload)

    # 2a. Resumed partial runtime transfer (.part only)
    resume_buf = io.StringIO()
    polystrata_prepare.prepare_polystrata(
        resume_out,
        data_dir=tmp_path / "data-resume",
        family="qwen36",
        model="UD-Q4_K_M",
        source=source_url,
        expected_sha256=sha256_hex,
        resume=True,
        client=_FakeClient(_range_handler),
        emitter=ProgressEmitter(enabled=True, stream=resume_buf, min_interval_seconds=0.0),
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(200 * GIB),
        venv_creator=_make_fake_venv,
    )
    resume_events = [json.loads(line) for line in resume_buf.getvalue().splitlines()]
    _assert_monotonic_four_steps(resume_events)
    resume_runtime_dl = [ev for ev in resume_events if ev["stage"] == "downloading_runtime"]
    assert resume_runtime_dl and all(
        ev.get("step") == 2 and ev.get("step_total") == 4 for ev in resume_runtime_dl
    )

    # 2b. Resumed existing runtime + Python 3.12 .venv -> reusing Python environment
    venv_py = resume_out / ".venv" / ("Scripts" if os.name == "nt" else "bin") / (
        "python.exe" if os.name == "nt" else "python"
    )
    if os.name == "nt":
        impl = resume_out / ".venv" / "_py312.py"
        impl.write_text(
            "import runpy, sys\n"
            "args = sys.argv[1:]\n"
            "if args and args[0] == '-c':\n"
            "    print('3.12')\n"
            "    sys.exit(0)\n"
            "idx = 1 if args and args[0] == '-u' else 0\n"
            "sys.argv = args[idx:]\n"
            "runpy.run_path(sys.argv[0], run_name='__main__')\n",
            encoding="utf-8",
        )
        venv_py.write_text(f'@echo off\r\n"{sys.executable}" "{impl}" %*\r\n', encoding="utf-8")
    else:
        venv_py.write_text(
            "#!/bin/sh\n"
            'if [ "$1" = "-c" ]; then\n'
            '  echo "3.12"\n'
            "  exit 0\n"
            "fi\n"
            f'exec "{sys.executable}" "$@"\n',
            encoding="utf-8",
        )
        os.chmod(venv_py, 0o755)
    reenter_buf = io.StringIO()
    polystrata_prepare.prepare_polystrata(
        resume_out,
        data_dir=tmp_path / "data-resume",
        family="qwen36",
        model="UD-Q4_K_M",
        source=source_url,
        expected_sha256=sha256_hex,
        resume=True,
        client=_FakeClient(_range_handler),
        emitter=ProgressEmitter(enabled=True, stream=reenter_buf, min_interval_seconds=0.0),
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(200 * GIB),
        venv_creator=_make_fake_venv,
    )
    reenter_events = [json.loads(line) for line in reenter_buf.getvalue().splitlines()]
    _assert_monotonic_four_steps(reenter_events)
    assert any(
        ev["stage"] == "setup_engine"
        and ev.get("step") == 3
        and ev.get("detail") == "reusing Python environment"
        for ev in reenter_events
    )


def test_parse_setup_line_maps_substeps_cmake_pip_and_weights_monotonically():
    buf = io.StringIO()
    em = ProgressEmitter(enabled=True, stream=buf, min_interval_seconds=0.0)
    st, step = "setup_engine", 3
    lines = [
        "=== Step 3: Python packages ===",
        "Installing collected packages: numpy, httpx",
        "=== Step 4: the PolyStrata engine ===",
        "[2/4] Building CXX object foo.o",
        "[100%] Linking CXX executable strata-poly",
        "=== Step 5: downloading Qwen3.6 weights ===",
        "  Qwen3.6.gguf: 1.50 / 3.00 GB (50%)",
        "waiting for checksum flush",  # plain line in downloading_model must not reset bytes
        "=== Step 6: Converting model pack ===",
        "Repacking tensor shard 1",
        "=== Step 7: writing start script ===",
    ]
    for line in lines:
        st, step = parse_setup_line(line, st, step, em)

    events = [json.loads(line) for line in buf.getvalue().splitlines()]
    _assert_monotonic_four_steps(events)
    assert any(
        ev["stage"] == "setup_engine"
        and ev["step"] == 3
        and ev["detail"] == "Installing collected packages: numpy, httpx"
        for ev in events
    )
    assert any(
        ev["stage"] == "downloading_model"
        and ev["step"] == 4
        and ev["downloaded_bytes"] == 1_500_000_000
        for ev in events
    )
    assert not any(ev.get("detail") == "waiting for checksum flush" for ev in events)
    assert any(
        ev["stage"] == "verifying"
        and ev["step"] == 4
        and ev["detail"] == "Repacking tensor shard 1"
        for ev in events
    )
    assert events[-1]["stage"] == "verifying"
    assert events[-1]["step"] == 4
    assert events[-1]["detail"] == "Step 7: writing start script"


def test_parse_setup_line_keeps_step6_downloads_and_sha256_verify_in_phase4():
    buf = io.StringIO()
    em = ProgressEmitter(enabled=True, stream=buf, min_interval_seconds=0.0)
    st, step = "setup_engine", 3
    lines = [
        "=== Step 5: downloading Qwen3.6 weights ===",
        "Qwen3.6.gguf: 4.20 / 4.20 GB (100.0%)",
        "checking Qwen3.6.gguf (SHA-256, 4.20 GB)",
        "draft.gguf: 100.00 / 500.00 MB (20.0%)",
        "=== Step 6: Configure runtime ===",
        "chat_template.jinja: 0.01 / 0.01 MB (100.0%)",
    ]
    for line in lines:
        st, step = parse_setup_line(line, st, step, em)

    events = [json.loads(line) for line in buf.getvalue().splitlines()]
    _assert_monotonic_four_steps(events)
    verify_ev = next(ev for ev in events if ev["stage"] == "verifying" and "SHA-256" in ev["detail"])
    assert verify_ev["step"] == 4
    assert verify_ev["step_total"] == 4
    assert verify_ev["total_bytes"] is None
    assert verify_ev["bytes_per_second"] is None

    draft_ev = next(ev for ev in events if ev.get("detail") == "draft.gguf")
    assert draft_ev["stage"] == "downloading_model"
    assert draft_ev["step"] == 4

    tmpl_ev = next(ev for ev in events if ev.get("detail") == "chat_template.jinja")
    assert tmpl_ev["stage"] == "downloading_model"
    assert tmpl_ev["step"] == 4
    assert tmpl_ev["step_total"] == 4
