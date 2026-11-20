"""Native Windows identity and evidence checks against owned synthetic processes."""

import asyncio
import os
import signal
import subprocess
import sys
from copy import deepcopy
from pathlib import Path
from types import SimpleNamespace

import pytest

import inferyard.platforms.identity as identity
from inferyard.analysis.environment import assess_environment
from inferyard.errors import EvidenceError, PreflightError
from inferyard.contracts.validation import json_bytes
from inferyard.evidence.storage import atomic_bytes, local_file
from inferyard.platforms.identity import environment_snapshot, hash_file, memory_available, process_start_ticks, static_preflight, verify_listener, verify_process
from inferyard.platforms.platform_io import open_nofollow
from inferyard.platforms.telemetry import read_rss, sample_memory
from inferyard.runtime.signals import install_termination_handler
from tests.helpers import readline_timeout

pytestmark = pytest.mark.skipif(sys.platform != "win32", reason="Native Windows APIs")


@pytest.fixture
def service(tmp_path):
    model = tmp_path / "模拟 model.gguf"
    model.write_bytes(b"synthetic model, not inference weights")
    worker = (
        "import socket,time; s=socket.socket(); s.bind(('127.0.0.1',0)); "
        "s.listen(); print(s.getsockname()[1],flush=True); time.sleep(30)"
    )
    arguments = ["-c", worker, "--model", str(model)]
    process = subprocess.Popen(
        [sys._base_executable, *arguments],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        encoding="utf-8",
    )
    try:
        port = int(readline_timeout(process))
        config = {
            "endpoint": {
                "server_pid": process.pid,
                "process_start_ticks": process_start_ticks(process.pid),
            },
            "engine": {"startup_args": arguments},
        }
        import psutil

        engine = hash_file(Path(psutil.Process(process.pid).exe()))
        yield process, port, config, hash_file(model), engine
    finally:
        if process.poll() is None:
            process.kill()
        process.wait(timeout=5)
        process.stdout.close()
        process.stderr.close()


@pytest.mark.parametrize("changed", ["pid", "ticks", "engine", "args", "model"])
def test_wrong_native_identity_blocks(service, changed):
    process, port, original, model, engine = service
    config = deepcopy(original)
    if changed == "pid":
        with pytest.raises(PreflightError, match="endpoint_pid_mismatch"):
            verify_listener(os.getpid(), "127.0.0.1", port)
        return
    if changed == "ticks":
        config["endpoint"]["process_start_ticks"] += 1
    elif changed == "engine":
        engine = model
    elif changed == "args":
        config["engine"]["startup_args"] = []
    else:
        model = engine
    with pytest.raises(PreflightError):
        verify_process(config, model, engine, "127.0.0.1", port)
