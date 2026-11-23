"""Unit tests for managed `inferyard engine prepare-polystrata` flows and CLI binding."""

from __future__ import annotations

import io
import json
from pathlib import Path

import pytest

from inferyard.application.polystrata_prepare import execute as execute_polystrata_prepare
from inferyard.cli.arguments import parser
from inferyard.cli.request import build_request
from inferyard.platforms.local_engines.detect import detect_local_engines
from inferyard.platforms.local_engines.installation import read_installation
from inferyard.provisioning import polystrata_prepare
from inferyard.provisioning.polystrata_archive import (
    DEFAULT_POLYSTRATA_SOURCE_BYTES,
    DEFAULT_POLYSTRATA_SOURCE_SHA256,
    DEFAULT_POLYSTRATA_SOURCE_URL,
    POLYSTRATA_PINNED_COMMIT,
)
from inferyard.provisioning.polystrata_preflight import GIB, run_polystrata_preflight
from inferyard.provisioning.preparation_io import PreparationError
from inferyard.provisioning.transfer_progress import ProgressEmitter
from polystrata_test_helpers import (
    _HAPPY_SETUP_PY,
    _FakeClient,
    _FakeResponse,
    _disk_usage,
    _make_fake_venv,
    _tar_gz_file,
    _zip_file,
)


def test_happy_path_local_tar_progress_log_sanitization_and_installation(tmp_path: Path):
    archive = _tar_gz_file(
        tmp_path / "PolyStrata-v0.1.45-poly.10.tar.gz",
        [
            ("dir", "PolyStrata-0.1.45-poly.10"),
            ("file", "PolyStrata-0.1.45-poly.10/setup.py", _HAPPY_SETUP_PY.encode("utf-8")),
            ("dir", "PolyStrata-0.1.45-poly.10/serve"),
            ("file", "PolyStrata-0.1.45-poly.10/serve/server.py", b"# fake server\n"),
        ],
    )
    out = tmp_path / "polystrata-install"
    data = tmp_path / "polystrata-data"
    progress_buf = io.StringIO()
    emitter = ProgressEmitter(enabled=True, stream=progress_buf, min_interval_seconds=0.0)

    details = polystrata_prepare.prepare_polystrata(
        out,
        data_dir=data,
        family="qwen36",
        model="UD-Q4_K_M",
        context=32768,
        model_source="modelscope",
        source=str(archive),
        proxy_origin="http://127.0.0.1:7890",
        emitter=emitter,
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(200 * GIB),
        venv_creator=_make_fake_venv,
    )

    assert details["ready_to_run"] is True
    assert details["version"] == "v0.1.45-poly.10"
    assert details["backend"] == "metal"
    assert Path(details["installation_path"]).is_file()
    assert Path(details["model_config_path"]).name == "strata-qwen36.json"
    assert details["resumed"] is False

    loaded = read_installation(details["installation_path"])
    assert loaded["family"] == "polystrata"
    assert loaded["working_directory"] == str(out.resolve())
    assert loaded["data_dir"] == str(data.resolve())
    from inferyard.config.local_engines.polystrata import resolve_polystrata_model

    resolved_cfg = resolve_polystrata_model(
        loaded,
        Path(details["model_config_path"]),
        tmp_path / "cand-out",
        None,
        served_model=None,
        system="Darwin",
        port=8080,
    )
    assert resolved_cfg[0].name == "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf"
    detected = detect_local_engines(extra_paths=[out])
    assert any(
        c["family"] == "polystrata" and c["working_directory"] == str(out.resolve())
        for c in detected["candidates"]
    )

    received_argv = json.loads((out / "received_argv.json").read_text(encoding="utf-8"))
    assert "--yes" in received_argv
    assert "--no-start" in received_argv
    assert ["--family", "qwen36"] == received_argv[received_argv.index("--family") : received_argv.index("--family") + 2]
    assert ["--model", "UD-Q4_K_M"] == received_argv[received_argv.index("--model") : received_argv.index("--model") + 2]
    assert ["--context", "32768"] == received_argv[received_argv.index("--context") : received_argv.index("--context") + 2]
    assert ["--source", "modelscope"] == received_argv[received_argv.index("--source") : received_argv.index("--source") + 2]
    received_env = json.loads((out / "received_env.json").read_text(encoding="utf-8"))
    assert received_env["HTTPS_PROXY"] == "http://127.0.0.1:7890"
    assert received_env["STRATA_SOURCE"] == "modelscope"
    for key in ("NO_PROXY", "no_proxy"):
        items = [part.strip() for part in (received_env.get(key) or "").split(",") if part.strip()]
        assert "modelscope.cn" in items
        assert ".modelscope.cn" in items

    events = [json.loads(line) for line in progress_buf.getvalue().splitlines()]
    stages = [ev["stage"] for ev in events]
    assert "preflight" in stages
    assert "downloading_runtime" in stages
    assert "setup_engine" in stages
    assert "downloading_model" in stages
    assert stages[-1] == "verifying"
    assert events[-1]["percent"] == 100.0
    dl_events = [ev for ev in events if ev["stage"] == "downloading_model" and ev.get("percent") == 50.0]
    assert dl_events and dl_events[0]["downloaded_bytes"] == 1_500_000_000

    log_text = Path(details["log_path"]).read_text(encoding="utf-8")
    assert "secret_token_1234567890" not in log_text
    assert "hf_abcdefghijklmnop" not in log_text
    assert "[REDACTED]" in log_text


def test_gguf_path_passthrough_linux_and_macos(tmp_path: Path):
    archive = _zip_file(
        tmp_path / "polystrata.zip",
        {
            "PolyStrata-0.1.45-poly.10/setup.py": _HAPPY_SETUP_PY.encode("utf-8"),
            "PolyStrata-0.1.45-poly.10/serve/server.py": b"# server\n",
        },
    )
    gguf_dir = tmp_path / "shared-gguf"
    gguf_dir.mkdir()
    gguf_file = gguf_dir / "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf"
    gguf_file.write_bytes(b"GGUF\x03\x00\x00\x00")

    # Linux -> --custom-gguf <file>
    out_linux = tmp_path / "ps-linux"
    polystrata_prepare.prepare_polystrata(
        out_linux,
        data_dir=tmp_path / "data-linux",
        gguf_path=gguf_file,
        backend="cuda",
        source=str(archive),
        platform_info=("Linux", "x64"),
        linux_venv_check=lambda _py: True,
        gpu_probe=lambda _sys: {
            "status": "available",
            "devices": [{"vendor": "nvidia", "driver_version": "580.65.06"}],
        },
        disk_usage_fn=_disk_usage(4 * GIB),
        venv_creator=_make_fake_venv,
    )
    argv_linux = json.loads((out_linux / "received_argv.json").read_text(encoding="utf-8"))
    idx = argv_linux.index("--custom-gguf")
    assert argv_linux[idx + 1] == str(gguf_file.resolve())
    assert "--backend" in argv_linux and argv_linux[argv_linux.index("--backend") + 1] == "cuda"

    # macOS -> --gguf-dir <parent_dir> --no-download-models plus resolved --family/--model
    out_mac = tmp_path / "ps-mac"
    polystrata_prepare.prepare_polystrata(
        out_mac,
        data_dir=tmp_path / "data-mac",
        gguf_path=gguf_file,
        backend="metal",
        source=str(archive),
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
        disk_usage_fn=_disk_usage(4 * GIB),
        venv_creator=_make_fake_venv,
    )
    argv_mac = json.loads((out_mac / "received_argv.json").read_text(encoding="utf-8"))
    idx_mac = argv_mac.index("--gguf-dir")
    assert argv_mac[idx_mac + 1] == str(gguf_dir.resolve())
    assert "--no-download-models" in argv_mac
    assert argv_mac[argv_mac.index("--family") + 1] == "qwen36"
    assert argv_mac[argv_mac.index("--model") + 1] == "UD-Q4_K_M"
    assert "--backend" not in argv_mac

    # Unmatched GGUF on macOS without explicit family/model fails closed in preflight
    unknown_gguf = gguf_dir / "unknown-custom.gguf"
    unknown_gguf.write_bytes(b"GGUF")
    with pytest.raises(PreparationError) as exc_unmatched:
        run_polystrata_preflight(
            tmp_path / "ps-mac-unknown",
            tmp_path / "data-mac-unknown",
            gguf_path=unknown_gguf,
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(20 * GIB),
        )
    assert exc_unmatched.value.reason == "polystrata_gguf_unmatched_model"


def test_archive_rejects_unsafe_entries_and_invalid_sources(tmp_path: Path):
    evil_tar = _tar_gz_file(
        tmp_path / "evil.tar.gz",
        [
            ("file", "setup.py", b"print(1)\n"),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
            ("symlink", "escape", "../../etc/passwd"),
        ],
    )
    with pytest.raises(PreparationError) as exc_info:
        polystrata_prepare.prepare_polystrata(
            tmp_path / "out-evil",
            data_dir=tmp_path / "data-evil",
            source=str(evil_tar),
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
        )
    assert exc_info.value.reason == "unsafe_archive_entry"

    # Reject plaintext HTTP URL
    with pytest.raises(PreparationError) as exc_http:
        polystrata_prepare.prepare_polystrata(
            tmp_path / "out-http",
            source="http://example.test/polystrata.tar.gz",
        )
    assert exc_http.value.reason == "invalid_engine_source"


def test_cli_argument_parsing_and_application_error_result(tmp_path: Path):
    args = parser().parse_args(
        [
            "engine",
            "prepare-polystrata",
            "--out",
            str(tmp_path / "ps"),
            "--data-dir",
            str(tmp_path / "data"),
            "--family",
            "qwen36",
            "--model",
            "UD-Q4_K_M",
            "--context",
            "32768",
            "--backend",
            "metal",
            "--model-source",
            "modelscope",
            "--gguf-path",
            str(tmp_path / "m.gguf"),
            "--python",
            "python3.12",
            "--source",
            "https://example.test/polystrata.tar.gz",
            "--proxy-origin",
            "http://127.0.0.1:7890",
            "--resume",
            "--progress-stream",
            "--timeout-seconds",
            "900",
        ]
    )
    req = build_request(args)
    assert req.command == "engine prepare-polystrata"
    assert req.family == "qwen36"
    assert req.polystrata_model == "UD-Q4_K_M"
    assert req.context_size == 32768
    assert req.polystrata_backend == "metal"
    assert req.polystrata_model_source == "modelscope"
    assert req.polystrata_python == "python3.12"
    assert req.gguf_path == (tmp_path / "m.gguf").absolute()
    assert req.resume_transfer is True
    assert req.progress_stream is True
    assert req.engine_timeout_seconds == 900.0

    # Application handler maps PreparationError to blocked CommandResult with details
    bad_args = parser().parse_args(
        [
            "engine",
            "prepare-polystrata",
            "--out",
            str(tmp_path / "ps-bad"),
            "--source",
            "http://insecure.example/archive.tar.gz",
        ]
    )
    code, result = execute_polystrata_prepare(build_request(bad_args))
    assert code == 2
    assert result.status == "blocked"
    assert result.limitations == ("invalid_engine_source",)


def test_verification_fails_when_engine_binary_or_config_missing(tmp_path: Path):
    # setup.py exits 0 but writes no engine binary
    no_engine_archive = _tar_gz_file(
        tmp_path / "no-engine.tar.gz",
        [
            ("file", "setup.py", b"print('done')\n"),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
        ],
    )
    with pytest.raises(PreparationError) as exc_eng:
        polystrata_prepare.prepare_polystrata(
            tmp_path / "out-no-eng",
            data_dir=tmp_path / "data-no-eng",
            source=str(no_engine_archive),
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            venv_creator=_make_fake_venv,
        )
    assert exc_eng.value.reason == "local_engine_polystrata_engine_missing"

    # setup.py creates engine binary but no strata-*.json config
    no_cfg_setup = """\
import os
from pathlib import Path
root = Path(__file__).resolve().parent
d = root / "engine-poly"
d.mkdir(exist_ok=True)
b = d / ("strata-poly.exe" if os.name == "nt" else "strata-poly")
b.write_text("#!/bin/sh\\n", encoding="utf-8")
if os.name != "nt":
    os.chmod(b, 0o755)
"""
    no_cfg_archive = _tar_gz_file(
        tmp_path / "no-cfg.tar.gz",
        [
            ("file", "setup.py", no_cfg_setup.encode("utf-8")),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
        ],
    )
    with pytest.raises(PreparationError) as exc_cfg:
        polystrata_prepare.prepare_polystrata(
            tmp_path / "out-no-cfg",
            data_dir=tmp_path / "data-no-cfg",
            source=str(no_cfg_archive),
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            venv_creator=_make_fake_venv,
        )
    assert exc_cfg.value.reason == "polystrata_config_missing"


def test_polystrata_default_source_pinning_and_modelscope_no_proxy_merge(monkeypatch, tmp_path: Path):
    assert POLYSTRATA_PINNED_COMMIT in DEFAULT_POLYSTRATA_SOURCE_URL
    assert DEFAULT_POLYSTRATA_SOURCE_BYTES == 25747544
    assert len(DEFAULT_POLYSTRATA_SOURCE_SHA256) == 64

    # Default remote source enforces pinned SHA-256 automatically and emits downloading_runtime first
    bad_payload = _tar_gz_file(
        tmp_path / "wrong-default.tar.gz",
        [
            ("file", "setup.py", _HAPPY_SETUP_PY.encode("utf-8")),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
        ],
    ).read_bytes()
    client = _FakeClient(lambda _m, _u, _h: _FakeResponse(bad_payload))
    progress_buf = io.StringIO()
    with pytest.raises(PreparationError) as exc_pin:
        polystrata_prepare.prepare_polystrata(
            tmp_path / "out-default-sha",
            data_dir=tmp_path / "data-default-sha",
            source=None,
            client=client,
            emitter=ProgressEmitter(enabled=True, stream=progress_buf, min_interval_seconds=0.0),
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            venv_creator=_make_fake_venv,
        )
    assert exc_pin.value.reason == "engine_checksum_mismatch"
    assert client.requests and client.requests[0][1] == DEFAULT_POLYSTRATA_SOURCE_URL
    stages = [json.loads(line)["stage"] for line in progress_buf.getvalue().splitlines()]
    assert "downloading_runtime" in stages

    # Verify Stage 2 uses proxy_origin while Stage 3 merges NO_PROXY cleanly for modelscope only
    archive = _tar_gz_file(
        tmp_path / "local-proxy.tar.gz",
        [
            ("file", "setup.py", _HAPPY_SETUP_PY.encode("utf-8")),
            ("dir", "serve"),
            ("file", "serve/server.py", b"# server\n"),
        ],
    )
    proxy_origins_seen: list[str | None] = []
    from inferyard.provisioning import polystrata_archive

    real_run_with_client = polystrata_archive.run_with_client

    def spy_run_with_client(timeout_seconds, active_client, action, *, proxy_origin=None):
        proxy_origins_seen.append(proxy_origin)
        return real_run_with_client(timeout_seconds, active_client, action, proxy_origin=proxy_origin)

    archive_bytes = archive.read_bytes()
    with monkeypatch.context() as mp:
        mp.setattr(polystrata_archive, "run_with_client", spy_run_with_client)
        mp.setenv("NO_PROXY", "localhost,127.0.0.1")
        mp.setenv("no_proxy", "10.0.0.0/8,modelscope.cn")

        out_remote = tmp_path / "out-remote-proxy"
        polystrata_prepare.prepare_polystrata(
            out_remote,
            data_dir=tmp_path / "data-remote-proxy",
            source="https://github.com/simplexai-labs/PolyStrata/archive/test.tar.gz",
            model_source="modelscope",
            proxy_origin="http://127.0.0.1:7890",
            client=_FakeClient(lambda _m, _u, _h: _FakeResponse(archive_bytes)),
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            venv_creator=_make_fake_venv,
        )
        assert proxy_origins_seen == ["http://127.0.0.1:7890"]
        ms_env = json.loads((out_remote / "received_env.json").read_text(encoding="utf-8"))
        assert ms_env["HTTPS_PROXY"] == "http://127.0.0.1:7890"
        assert ms_env["NO_PROXY"] == "localhost,127.0.0.1,10.0.0.0/8,modelscope.cn,.modelscope.cn"
        assert ms_env["no_proxy"] == "localhost,127.0.0.1,10.0.0.0/8,modelscope.cn,.modelscope.cn"

        out_hf = tmp_path / "out-hf-proxy"
        polystrata_prepare.prepare_polystrata(
            out_hf,
            data_dir=tmp_path / "data-hf-proxy",
            source=str(archive),
            model_source="huggingface",
            proxy_origin="http://127.0.0.1:7890",
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            venv_creator=_make_fake_venv,
        )
        hf_env = json.loads((out_hf / "received_env.json").read_text(encoding="utf-8"))
        assert hf_env["HTTPS_PROXY"] == "http://127.0.0.1:7890"
        assert hf_env["NO_PROXY"] == "localhost,127.0.0.1"
