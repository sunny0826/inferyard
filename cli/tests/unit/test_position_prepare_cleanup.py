"""Owned server cleanup must survive adapter failure or cancellation."""

import asyncio
import importlib.util
import json
import subprocess
import sys
from pathlib import Path

import pytest

SPEC = importlib.util.spec_from_file_location(
    "position_prepare", Path(__file__).parents[2] / "scripts/verify_real_position_prepare.py"
)
module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(module)


@pytest.mark.parametrize("failure", [RuntimeError, asyncio.CancelledError])
def test_adapter_failure_still_stops_real_owned_process(tmp_path, failure):
    class Adapter:
        async def close(self):
            raise failure("adapter close failed")

    process = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(60)"])
    try:
        with pytest.raises(failure):
            asyncio.run(module.close_owned_service(Adapter(), process, tmp_path))
        assert process.poll() is not None
        lifecycle = json.loads((tmp_path / "lifecycle.json").read_text())
        assert lifecycle["stopped"] is True
        assert lifecycle["owned_pid"] == process.pid
        assert "lifecycle.json" in json.loads((tmp_path / "manifest.json").read_text())["files"]
    finally:
        if process.poll() is None:
            process.kill()
            process.wait(timeout=5)
