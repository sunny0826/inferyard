"""Tests for pinned PolyStrata archive SHA-256 enforcement and proxy/ModelScope NO_PROXY routing."""

from __future__ import annotations

import io
import json
from pathlib import Path
import urllib.request

import httpx
import pytest

from inferyard.provisioning import polystrata_archive, polystrata_prepare
from inferyard.provisioning.acquire_errors import classify_httpx_error
from inferyard.provisioning.polystrata_archive import (
    DEFAULT_POLYSTRATA_SOURCE_BYTES,
    DEFAULT_POLYSTRATA_SOURCE_SHA256,
    DEFAULT_POLYSTRATA_SOURCE_URL,
    POLYSTRATA_PINNED_COMMIT,
)
from inferyard.provisioning.polystrata_preflight import GIB
from inferyard.provisioning.preparation_io import PreparationError
from inferyard.provisioning.transfer_progress import ProgressEmitter
from polystrata_test_helpers import (
    _HAPPY_SETUP_PY,
    _FakeClient,
    _FakeResponse,
    _disk_usage,
    _make_fake_venv,
    _tar_gz_file,
)


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

    # Supplying a conflicting --expected-sha256 for the default source fails before network I/O
    conflict_client = _FakeClient(lambda _m, _u, _h: _FakeResponse(bad_payload))
    with pytest.raises(PreparationError) as exc_conflict:
        polystrata_prepare.prepare_polystrata(
            tmp_path / "out-conflict-sha",
            data_dir=tmp_path / "data-conflict-sha",
            source=None,
            expected_sha256="0" * 64,
            client=conflict_client,
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            venv_creator=_make_fake_venv,
        )
    assert exc_conflict.value.reason == "engine_checksum_mismatch"
    assert conflict_client.requests == []

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

        # Existing wildcard NO_PROXY="*" must stay "*" so global bypass behavior is preserved
        mp.setenv("NO_PROXY", "*")
        mp.delenv("no_proxy", raising=False)
        out_star = tmp_path / "out-star-proxy"
        polystrata_prepare.prepare_polystrata(
            out_star,
            data_dir=tmp_path / "data-star-proxy",
            source=str(archive),
            model_source="modelscope",
            proxy_origin="http://127.0.0.1:7890",
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
            disk_usage_fn=_disk_usage(200 * GIB),
            venv_creator=_make_fake_venv,
        )
        star_env = json.loads((out_star / "received_env.json").read_text(encoding="utf-8"))
        assert star_env["NO_PROXY"] == "*"
        assert star_env["no_proxy"] == "*"
        assert (
            urllib.request.proxy_bypass_environment(
                "github.com", {"no": star_env["no_proxy"]}
            )
            is True
        )

        # Proxy CONNECT failures classify as connection_error (triggering Stage 2 proxy guidance)
        assert classify_httpx_error(httpx.ProxyError("502 Bad Gateway")) == "connection_error"

        # NO_PROXY="*" shadowed by lowercase no_proxy="localhost" must NOT become "*"
        # and no_proxy="localhost,*" must preserve original routing for github.com while bypassing modelscope.cn
        for case_name, upper_val, lower_val in (
            ("shadowed-star", "*", "localhost"),
            ("list-with-star", None, "localhost,*"),
        ):
            mp.setenv("NO_PROXY", upper_val or "")
            if upper_val is None:
                mp.delenv("NO_PROXY", raising=False)
            mp.setenv("no_proxy", lower_val)
            out_case = tmp_path / f"out-{case_name}"
            polystrata_prepare.prepare_polystrata(
                out_case,
                data_dir=tmp_path / f"data-{case_name}",
                source=str(archive),
                model_source="modelscope",
                proxy_origin="http://127.0.0.1:7890",
                platform_info=("Darwin", "arm64"),
                macos_version="15.4",
                xcode_clt_check=lambda: True,
                disk_usage_fn=_disk_usage(200 * GIB),
                venv_creator=_make_fake_venv,
            )
            case_env = json.loads((out_case / "received_env.json").read_text(encoding="utf-8"))
            assert (
                urllib.request.proxy_bypass_environment("github.com", {"no": case_env["no_proxy"]})
                is False
            )
            assert (
                urllib.request.proxy_bypass_environment(
                    "www.modelscope.cn", {"no": case_env["no_proxy"]}
                )
                is True
            )
