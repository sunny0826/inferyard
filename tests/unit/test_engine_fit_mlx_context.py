"""Controlled MLX context admission with synthetic dependencies, never real model loading."""

import json
import queue as queues
import sys
import threading
from contextlib import nullcontext
from types import SimpleNamespace

import pytest

from tests.unit.test_engine_fit_mlx_server import (
    drain,
    generations,
    mlx_server,
    payload,
    request,
    server,
    until,
)


@pytest.fixture
def backend_factory(tmp_path, monkeypatch):
    state = {"prompt_tokens": 480, "templates": [], "generations": [], "syncs": []}
    stream = object()

    def template(messages, **options):
        state["templates"].append((messages, options))
        return list(range(state["prompt_tokens"]))

    tokenizer = SimpleNamespace(apply_chat_template=template)

    def generate(model, supplied_tokenizer, **options):
        state["generations"].append((model, supplied_tokenizer, options))
        return (
            SimpleNamespace(
                text="42",
                generation_tokens=1,
                prompt_tokens=len(options["prompt"]),
                finish_reason="stop",
            )
            for _ in range(1)
        )

    mx = SimpleNamespace(
        gpu="synthetic-gpu",
        new_thread_local_stream=lambda _device: stream,
        stream=lambda active: nullcontext(active),
        synchronize=lambda active: state["syncs"].append(active),
    )
    monkeypatch.setitem(sys.modules, "mlx", SimpleNamespace(core=mx))
    monkeypatch.setitem(sys.modules, "mlx.core", mx)
    monkeypatch.setitem(
        sys.modules,
        "mlx_lm",
        SimpleNamespace(
            load=lambda *_args, **_kwargs: ("synthetic", tokenizer), stream_generate=generate
        ),
    )
    monkeypatch.setitem(
        sys.modules, "mlx_lm.sample_utils", SimpleNamespace(make_sampler=lambda **kwargs: kwargs)
    )
    monkeypatch.setattr(mlx_server, "version", lambda _package: "0.0.0+synthetic")
    monkeypatch.setenv("HF_HUB_OFFLINE", "1")
    monkeypatch.setenv("TRANSFORMERS_OFFLINE", "1")

    def make(max_model_len=None):
        return mlx_server.MLXBackend(tmp_path, max_model_len=max_model_len), state

    return make


@pytest.mark.parametrize("value", [0, -1, 32769, True, False, 512.0, "512"])
def test_invalid_context_limit_is_rejected_before_model_loading(tmp_path, value):
    with pytest.raises(ValueError, match="invalid_max_model_len"):
        mlx_server.MLXBackend(tmp_path, max_model_len=value)


def test_http_rejects_total_context_before_admission_and_remains_usable(backend_factory):
    backend, state = backend_factory(512)
    body = payload()
    body["max_tokens"] = 32
    state["prompt_tokens"] = 481
    with server(backend) as instance:
        code, raw = request(instance, "POST", "/v1/chat/completions", body)
        assert code == 400 and json.loads(raw) == {"error": "invalid_request"}
        assert state["generations"] == []
        assert instance.generations.metrics() == (0, 0, 0)
        assert instance.generations.jobs.empty()
        code, raw = request(instance, "GET", "/version")
        details = json.loads(raw)
        assert code == 200 and details["max_model_len"] == 512
        assert details["engine_fit_protocol"] == "mlx-lm.v1"
        assert details["engine_fit_source_sha256"] == instance.source_hash
        state["prompt_tokens"] = 480
        code, raw = request(instance, "POST", "/v1/chat/completions", body)
        assert code == 200 and raw.endswith(b"data: [DONE]\n\n")
        assert instance.generations.metrics() == (0, 0, 0)
    assert len(state["generations"]) == 1
    assert state["generations"][0][2]["max_tokens"] == 32
    assert len(state["generations"][0][2]["prompt"]) == 480
    # Both HTTP requests tokenize once; the worker reuses the admitted token list.
    assert len(state["templates"]) == 2
    assert state["templates"][0][1] == {"tokenize": True, "add_generation_prompt": True}


@pytest.mark.parametrize("prompt_tokens", [0, 32769])
def test_configured_context_rejects_invalid_tokenization_before_admission(
    backend_factory, prompt_tokens
):
    backend, state = backend_factory(32768)
    state["prompt_tokens"] = prompt_tokens
    with generations(backend) as queue:
        with pytest.raises(ValueError, match="prompt_tokens_out_of_bounds"):
            queue.submit("synthetic prompt", 32)
        assert queue.metrics() == (0, 0, 0) and queue.jobs.empty()
        assert state["generations"] == []


def test_rejected_context_does_not_poison_an_active_generation(backend_factory):
    backend, state = backend_factory(512)
    release = threading.Event()
    started = threading.Event()
    original = backend.generate

    def generate(*args, **kwargs):
        started.set()
        assert release.wait(3)
        yield from original(*args, **kwargs)

    backend.generate = generate
    with generations(backend) as queue:
        accepted = queue.submit("accepted", 32)
        try:
            assert started.wait(3)
            assert queue.metrics() == (1, 0, 0)
            state["prompt_tokens"] = 481
            with pytest.raises(ValueError, match="context_length_exceeded"):
                queue.submit("rejected", 32)
            assert queue.metrics() == (1, 0, 0) and queue.jobs.empty()
        finally:
            release.set()
        assert drain(accepted)[-1][0] == "done"
        until(lambda: queue.metrics() == (0, 0, 0))
    assert len(state["generations"]) == 1


def _blocked_preparation(backend):
    started, release, calls = threading.Event(), threading.Event(), []
    original = backend.prepare_prompt

    def prepare(prompt, max_tokens):
        if isinstance(prompt, str):
            calls.append(prompt)
            started.set()
            assert release.wait(3)
        return original(prompt, max_tokens)

    backend.prepare_prompt = prepare
    return started, release, calls


def _background(call):
    result = queues.Queue()

    def invoke():
        try:
            result.put(call())
        except Exception as exc:
            result.put(exc)

    thread = threading.Thread(target=invoke, daemon=True)
    thread.start()
    return thread, result


@pytest.mark.parametrize("prompt_tokens,expected", [(480, 200), (481, 400)])
def test_preparation_is_visible_without_blocking_readonly_endpoints(
    backend_factory, prompt_tokens, expected
):
    backend, state = backend_factory(512)
    state["prompt_tokens"] = prompt_tokens
    started, release, _ = _blocked_preparation(backend)
    body = payload()
    body["max_tokens"] = 32
    with server(backend) as instance:
        thread, result = _background(
            lambda: request(instance, "POST", "/v1/chat/completions", body)
        )
        try:
            assert started.wait(3)
            code, raw = request(instance, "GET", "/metrics")
            assert code == 200 and b"engine_fit:num_requests_waiting 1\n" in raw
            assert b"engine_fit:num_requests_running 0\n" in raw
            assert request(instance, "GET", "/version")[0] == 200
            assert request(instance, "GET", "/v1/models")[0] == 200
            assert state["generations"] == []
        finally:
            release.set()
        thread.join(timeout=3)
        assert not thread.is_alive()
        assert result.get_nowait()[0] == expected
        assert instance.generations.metrics() == (0, 0, 0)
    assert len(state["generations"]) == int(expected == 200)


@pytest.mark.parametrize("stop", ["close", "poison"])
def test_preparation_cannot_enqueue_after_close_or_poison(backend_factory, stop):
    backend, state = backend_factory(512)
    started, release, _ = _blocked_preparation(backend)
    with generations(backend) as queue:
        thread, result = _background(lambda: queue.submit("synthetic", 32))
        try:
            assert started.wait(3)
            assert queue.metrics() == (0, 1, 0)
            if stop == "close":
                queue.close()
            else:
                with queue.lock:
                    queue.poisoned = True
            assert queue.metrics() == (0, 1, 1)
        finally:
            release.set()
        thread.join(timeout=3)
        assert not thread.is_alive()
        error = result.get_nowait()
        assert isinstance(error, RuntimeError) and str(error) == "service_poisoned"
        assert queue.metrics() == (0, 0, 1) and queue.jobs.empty()
    assert state["generations"] == []


def test_preparation_reserves_bounded_capacity_and_serializes_tokenization(backend_factory):
    backend, state = backend_factory(512)
    started, release, calls = _blocked_preparation(backend)
    with generations(backend) as queue:
        pending = [_background(lambda: queue.submit("synthetic", 32)) for _ in range(4)]
        try:
            assert started.wait(3)
            until(lambda: queue.metrics() == (0, 4, 0))
            assert len(calls) == 1
            with pytest.raises(queues.Full):
                queue.submit("over capacity", 32)
            assert queue.metrics() == (0, 4, 0)
        finally:
            release.set()
        for thread, result in pending:
            thread.join(timeout=3)
            assert not thread.is_alive()
            assert drain(result.get_nowait())[-1][0] == "done"
        assert queue.metrics() == (0, 0, 0)
    assert len(state["generations"]) == 4


def test_omitted_context_retains_original_input_bound_and_generation_path(backend_factory):
    backend, state = backend_factory()
    state["prompt_tokens"] = 32768
    assert backend.max_model_len is None
    list(backend.responses("synthetic", 8192))
    assert len(state["generations"]) == 1
    assert len(state["generations"][0][2]["prompt"]) == 32768
    state["prompt_tokens"] = 32769
    with pytest.raises(ValueError, match="prompt_tokens_out_of_bounds"):
        backend.responses("synthetic", 1)
    assert len(state["generations"]) == 1


@pytest.mark.parametrize("max_model_len", [None, 1, 512, 32768])
def test_cli_forwards_optional_context_to_backend(tmp_path, monkeypatch, max_model_len):
    observed = []
    argv = [
        "mlx_server.py",
        "--model",
        str(tmp_path),
        "--port",
        "8080",
        "--served-model-name",
        "synthetic",
    ]
    if max_model_len is not None:
        argv.extend(["--max-model-len", str(max_model_len)])
    monkeypatch.setattr(sys, "argv", argv)
    monkeypatch.setattr(sys, "platform", "darwin")
    monkeypatch.setattr(
        mlx_server,
        "MLXBackend",
        lambda path, **options: observed.append((path, options)) or "synthetic-backend",
    )
    monkeypatch.setattr(
        mlx_server,
        "DiagnosticServer",
        lambda *_args: nullcontext(SimpleNamespace(serve_forever=lambda: None)),
    )
    assert mlx_server.main() == 0
    assert observed == [(tmp_path, {"max_model_len": max_model_len})]


@pytest.mark.parametrize("value", ["0", "32769"])
def test_cli_rejects_out_of_range_context_without_loading(tmp_path, monkeypatch, capsys, value):
    monkeypatch.setattr(sys, "platform", "darwin")
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "mlx_server.py",
            "--model",
            str(tmp_path),
            "--port",
            "8080",
            "--served-model-name",
            "synthetic",
            "--max-model-len",
            value,
        ],
    )
    assert mlx_server.main() == 2
    assert capsys.readouterr().err == "mlx_engine_fit_server_startup_or_runtime_failed\n"
