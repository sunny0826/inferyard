"""Synthetic formal MLX HTTP checks; no model or MLX installation is used."""

import importlib.util
import json
import threading
from contextlib import contextmanager
from pathlib import Path

from tests.unit.test_engine_fit_mlx_server import SyntheticBackend, request

SCRIPT = Path(__file__).parents[2] / "src/inferyard/data/local_engine/mlx_server.py"
SPEC = importlib.util.spec_from_file_location("synthetic_formal_mlx", SCRIPT)
formal = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(formal)


def payload(stream=False):
    body = {
        "model": "synthetic",
        "messages": [{"role": "user", "content": "What is 17 + 25?"}],
        "max_tokens": 8,
        "temperature": 0,
        "stream": stream,
    }
    if stream:
        body["stream_options"] = {"include_usage": True}
    return body


@contextmanager
def server(backend=None, key=None):
    backend = backend or SyntheticBackend()
    backend.max_model_len = getattr(backend, "max_model_len", 512)
    backend.prepare_prompt = getattr(backend, "prepare_prompt", lambda prompt, _tokens: prompt)
    instance = formal.FormalServer(("127.0.0.1", 0), backend, "synthetic", key)
    thread = threading.Thread(target=instance.serve_forever, daemon=True)
    thread.start()
    try:
        yield instance
    finally:
        instance.shutdown()
        instance.server_close()
        thread.join(timeout=2)


def test_standalone_python312_and_context_required_before_load(tmp_path, monkeypatch):
    import ast
    import sys

    import pytest

    ast.parse(SCRIPT.read_text(), feature_version=(3, 12))
    for value in (None, 0, 32769, True, "512"):
        with pytest.raises(ValueError, match="invalid_max_model_len"):
            formal.MLXBackend(tmp_path, value)
    calls = []
    monkeypatch.setattr(formal, "MLXBackend", lambda *_a, **_k: calls.append(True))
    argv = [
        str(SCRIPT),
        "--model",
        str(tmp_path),
        "--port",
        "8080",
        "--served-model-name",
        "synthetic",
    ]
    monkeypatch.setattr(sys, "argv", argv)
    with pytest.raises(SystemExit) as error:
        formal.main()
    assert error.value.code == 2
    monkeypatch.setattr(sys, "argv", argv + ["--max-model-len", "0"])
    assert formal.main() == 2
    assert calls == []


def test_nonlocal_server_and_model_rejected(tmp_path):
    import pytest

    backend = SyntheticBackend()
    backend.max_model_len = 512
    with pytest.raises(ValueError, match="loopback_required"):
        formal.FormalServer(("0.0.0.0", 0), backend, "synthetic")
    with pytest.raises(ValueError, match="local_model_directory_required"):
        formal.MLXBackend(tmp_path / "missing", 512)


def test_standalone_entry_works_outside_checkout_without_pythonpath(tmp_path):
    import subprocess
    import sys

    result = subprocess.run(
        [sys.executable, "-I", str(SCRIPT), "--help"],
        cwd=tmp_path,
        capture_output=True,
        text=True,
        timeout=5,
        check=False,
    )
    assert result.returncode == 0, result.stderr
    assert "--max-model-len" in result.stdout
