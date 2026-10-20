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


def test_formal_ordinary_request_returns_message_and_usage():
    with server() as instance:
        status, raw = request(instance, "POST", "/v1/chat/completions", payload())
        assert status == 200, raw
        body = json.loads(raw)
        assert body["choices"] == [
            {"index": 0, "message": {"role": "assistant", "content": "42"}, "finish_reason": "stop"}
        ]
        assert body["usage"] == {"prompt_tokens": 7, "completion_tokens": 1, "total_tokens": 8}


def test_stream_and_ordinary_share_queue_backend_and_identity():
    import hashlib

    backend = SyntheticBackend()
    with server(backend) as instance:
        assert isinstance(instance.generations, formal.legacy.GenerationQueue)
        for stream in (False, True):
            status, raw = request(instance, "POST", "/v1/chat/completions", payload(stream))
            assert status == 200
            if stream:
                assert raw.endswith(b"data: [DONE]\n\n")
                chunks = [
                    json.loads(line[6:]) for line in raw.splitlines() if line.startswith(b"data: {")
                ]
                assert chunks[-2]["choices"][0]["finish_reason"] == "stop"
                assert chunks[-1]["usage"]["total_tokens"] == 8
        details = json.loads(request(instance, "GET", "/version")[1])
        assert details == {
            "version": "0.0.0+synthetic",
            "local_engine_protocol": "mlx-lm-formal.v1",
            "local_engine_source_sha256": hashlib.sha256(SCRIPT.read_bytes()).hexdigest(),
            "generation_queue_source_sha256": hashlib.sha256(
                formal.DEPENDENCY.read_bytes()
            ).hexdigest(),
            "synthetic": True,
            "max_model_len": 512,
        }
        assert backend.calls == [("What is 17 + 25?", 8)] * 2
        assert b"engine_fit:poisoned 0\n" in request(instance, "GET", "/metrics")[1]


def test_authentication_rejects_generation_and_identity_without_key():
    backend = SyntheticBackend()
    with server(backend, key="test-secret") as instance:
        for path in ("/version", "/metrics", "/v1/models"):
            assert request(instance, "GET", path)[0] == 401
        assert request(instance, "POST", "/v1/chat/completions", payload())[0] == 401
        assert backend.calls == []
        assert (
            request(
                instance,
                "POST",
                "/v1/chat/completions",
                payload(),
                {"Authorization": "Bearer test-secret"},
            )[0]
            == 200
        )


def test_invalid_wire_shapes_never_enter_backend():
    import http.client

    backend = SyntheticBackend()
    invalid = [
        {**payload(), "stream": 0},
        {**payload(), "stream_options": {"include_usage": True}},
        {**payload(), "stream": True},
        {**payload(True), "stream_options": {"include_usage": 1}},
        {**payload(), "max_tokens": True},
        {**payload(), "temperature": True},
        {**payload(), "temperature": 0.1},
        {**payload(), "model": "other"},
        {**payload(), "top_p": 1},
        {**payload(), "temperature": float("inf")},
        {**payload(), "max_tokens": 8193},
        {**payload(), "messages": [{"role": "system", "content": "hello"}]},
    ]
    with server(backend) as instance:
        for body in invalid:
            assert request(instance, "POST", "/v1/chat/completions", body)[0] == 400
        for raw in ('{"model":1,"model":2}', '{"temperature":NaN}', '{"temperature":1e999}'):
            client = http.client.HTTPConnection(*instance.server_address, timeout=3)
            client.request(
                "POST", "/v1/chat/completions", raw, {"Content-Type": "application/json"}
            )
            result = client.getresponse()
            assert result.status == 400
            result.read()
            client.close()
        assert backend.calls == []
        assert instance.generations.metrics() == (0, 0, 0)


def test_context_limit_error_is_explicit_and_does_not_poison():
    backend = SyntheticBackend()
    backend.max_model_len = 8

    def prepare(prompt, tokens):
        if len(prompt) + tokens > backend.max_model_len:
            raise ValueError("context_length_exceeded")
        return prompt

    backend.prepare_prompt = prepare
    with server(backend) as instance:
        status, raw = request(instance, "POST", "/v1/chat/completions", payload())
        assert status == 400 and json.loads(raw) == {"error": "context_length_exceeded"}
        assert backend.calls == []
        assert instance.generations.metrics() == (0, 0, 0)
        body = {**payload(), "messages": [{"role": "user", "content": "ok"}], "max_tokens": 6}
        assert request(instance, "POST", "/v1/chat/completions", body)[0] == 200


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
