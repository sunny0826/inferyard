"""Opt-in closed-loop e2e: the CLI's own acquire commands perform the real
downloads, then the full benchmark chain runs from the downloaded assets.

Set LAB_E2E_CLOSED_LOOP=1 with LAB_E2E_MODEL_SOURCE (a pinned HF/ModelScope
GGUF blob URL) and LAB_E2E_ENGINE_SOURCE (an engine archive URL). The host
must be on AC power and able to reach both sources directly (the acquire
clients ignore proxies); nothing is retried, so a network failure fails the
test. The chain itself is the proven packaging entry tests/packaging/
ci_model_e2e.py, invoked as a subprocess: engine assembly, service start and
shutdown, zh-smoke + zh-svg-pelican runs, report and offline verify.
"""

import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

MODEL_ENV = "LAB_E2E_MODEL_SOURCE"
ENGINE_ENV = "LAB_E2E_ENGINE_SOURCE"
HARDWARE_STOPS = ("temperature_safety_threshold_reached",)
TIMEOUT_SECONDS = "3600"

pytestmark = pytest.mark.skipif(
    os.environ.get("LAB_E2E_CLOSED_LOOP") != "1"
    or not os.environ.get(MODEL_ENV)
    or not os.environ.get(ENGINE_ENV),
    reason=(
        f"opt-in real-download closed loop: LAB_E2E_CLOSED_LOOP=1 with {MODEL_ENV} and {ENGINE_ENV}"
    ),
)


def cli(args):
    return subprocess.run(
        [sys.executable, "-m", "inferyard", *args],
        capture_output=True,
        text=True,
        check=False,
    )


def acquire(command, source, out):
    completed = cli(
        [
            command,
            "acquire",
            "--source",
            source,
            "--out",
            str(out),
            "--timeout-seconds",
            TIMEOUT_SECONDS,
        ]
    )
    assert completed.returncode == 0, completed.stderr
    return json.loads(completed.stdout)["details"]


def assert_record_matches_download(record_path, downloaded_file, details):
    record = json.loads(Path(record_path).read_text(encoding="utf-8"))
    digest = hashlib.sha256()
    with Path(downloaded_file).open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    assert details["sha256"] == digest.hexdigest()
    assert record["sha256"] == digest.hexdigest()
    assert record["bytes"] == details["bytes"]


def test_real_downloads_drive_the_full_benchmark_chain(tmp_path):
    downloads = tmp_path / "downloads"
    downloads.mkdir()
    model = acquire("model", os.environ[MODEL_ENV], downloads / "model")
    engine = acquire("engine", os.environ[ENGINE_ENV], downloads / "engine")
    assert_record_matches_download(model["source_record"], model["model"], model)
    assert_record_matches_download(engine["source_record"], engine["engine"], engine)

    chain = subprocess.run(
        [
            sys.executable,
            "tests/packaging/ci_model_e2e.py",
            "--model",
            model["model"],
            "--engine-archive",
            engine["engine"],
            "--model-source",
            model["source_record"],
            "--out",
            str(tmp_path / "e2e"),
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    if chain.returncode == 0:
        summary = json.loads(chain.stdout.strip().splitlines()[-1])
        assert summary["verified_after_cleanup"] is True
        assert summary["smoke_quality"]["denominator"] == 40
        return
    # A native hardware stop keeps its incomplete evidence and is never retried.
    if any(stop in chain.stdout + chain.stderr for stop in HARDWARE_STOPS):
        pytest.skip(f"hardware safety stop retained as incomplete evidence: {chain.stderr.strip()}")
    raise AssertionError(
        f"closed-loop chain failed with {chain.returncode}\nstderr: {chain.stderr.strip()}"
    )
