"""Synthetic MLX protocol/lifecycle checks; no MLX import or real model execution."""

import ast
import copy
import http.client
import importlib.util
import json
import queue
import socket
import struct
import sys
import threading
import time
from contextlib import contextmanager, nullcontext
from pathlib import Path
from types import SimpleNamespace

import pytest

SCRIPT = Path(__file__).parents[2] / "src/inferyard/data/engine_fit/mlx_server.py"
SPEC = importlib.util.spec_from_file_location("synthetic_engine_fit_mlx_server", SCRIPT)
mlx_server = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(mlx_server)


def response(text="42", finish="stop", count=1):
    return SimpleNamespace(
        text=text, finish_reason=finish, generation_tokens=count, prompt_tokens=7
    )


class SyntheticBackend:
    version = "0.0.0+synthetic"
    synthetic = True

    def __init__(self, produce=None, synchronize=None):
        self.produce = produce or (lambda _prompt, _tokens: iter((response(),)))
        self.sync = synchronize or (lambda: None)
        self.calls = []

    def responses(self, prompt, max_tokens):
        self.calls.append((prompt, max_tokens))
        source = self.produce(prompt, max_tokens)
        yield from source

    def synchronize(self):
        self.sync()


@contextmanager
def generations(backend):
    state = mlx_server.GenerationQueue(backend)
    try:
        yield state
    finally:
        state.close()


def drain(job):
    items = []
    while True:
        item = job.output.get(timeout=3)
        items.append(item)
        if item[0] in ("error", "done"):
            return items


def until(predicate):
    deadline = time.monotonic() + 3
    while not predicate():
        if time.monotonic() >= deadline:
            raise AssertionError("synthetic lifecycle did not reach expected state")
        time.sleep(0.005)


def payload():
    return {
        "model": "synthetic",
        "messages": [{"role": "user", "content": "What is 17 + 25?"}],
        "max_tokens": 8,
        "temperature": 0,
        "stream": True,
        "stream_options": {"include_usage": True},
    }


def test_single_generation_has_usage_and_becomes_idle_after_sync():
    order = []

    def produce(_prompt, _tokens):
        try:
            yield response("4", finish=None)
            yield response("2", count=2)
        finally:
            order.append("close")

    backend = SyntheticBackend(produce, lambda: order.append("sync"))
    with generations(backend) as state:
        assert state.metrics() == (0, 0, 0)
        result = drain(state.submit("math", 8))
        assert result == [("text", "4"), ("text", "2"), ("done", ("stop", 7, 2))]
        assert order == ["close", "sync"]
        assert state.metrics() == (0, 0, 0)


def test_sync_is_covered_by_running_and_delays_terminal_marker():
    entered, release = threading.Event(), threading.Event()

    def synchronize():
        entered.set()
        assert release.wait(3)

    with generations(SyntheticBackend(synchronize=synchronize)) as state:
        job = state.submit("math", 8)
        try:
            assert entered.wait(3)
            assert state.metrics() == (1, 0, 0)
            assert job.output.get(timeout=1) == ("text", "42")
            with pytest.raises(queue.Empty):
                job.output.get_nowait()
        finally:
            release.set()
        assert drain(job) == [("done", ("stop", 7, 1))]


def test_queued_work_is_counted_and_queue_is_bounded():
    entered, release = threading.Event(), threading.Event()

    def produce(_prompt, _tokens):
        entered.set()
        assert release.wait(3)
        yield response()

    with generations(SyntheticBackend(produce)) as state:
        first = state.submit("first", 8)
        try:
            assert entered.wait(3)
            jobs = [state.submit(str(index), 8) for index in range(4)]
            assert state.metrics() == (1, 4, 0)
            with pytest.raises(queue.Full):
                state.submit("overflow", 8)
            assert state.metrics() == (1, 4, 0)
        finally:
            release.set()
        assert drain(first)[-1][0] == "done"
        for job in jobs:
            assert drain(job)[-1][0] == "done"
        assert state.metrics() == (0, 0, 0)


def test_generation_failure_poison_is_persistent():
    def produce(_prompt, _tokens):
        raise ValueError("secret backend error must not escape")

    with generations(SyntheticBackend(produce)) as state:
        assert drain(state.submit("math", 8)) == [("error", None)]
        assert state.metrics() == (0, 0, 1)
        with pytest.raises(RuntimeError, match="service_poisoned"):
            state.submit("again", 8)


def test_sync_failure_keeps_running_and_poisoned():
    def synchronize():
        raise RuntimeError("gpu synchronization uncertain")

    with generations(SyntheticBackend(synchronize=synchronize)) as state:
        assert drain(state.submit("math", 8))[-1] == ("error", None)
        assert state.metrics() == (1, 0, 1)


def test_iterator_close_failure_cannot_claim_idle():
    class BrokenClose:
        def __iter__(self):
            return iter((response(),))

        def close(self):
            raise RuntimeError("cleanup uncertain")

    backend = SyntheticBackend()
    backend.responses = lambda _prompt, _tokens: BrokenClose()
    with generations(backend) as state:
        assert drain(state.submit("math", 8))[-1] == ("error", None)
        assert state.metrics() == (1, 0, 1)


def test_disconnect_does_not_clear_active_generation_before_sync():
    entered, release, syncing, release_sync = (threading.Event() for _ in range(4))

    def produce(_prompt, _tokens):
        entered.set()
        assert release.wait(3)
        yield response()

    def synchronize():
        syncing.set()
        assert release_sync.wait(3)

    with generations(SyntheticBackend(produce, synchronize)) as state:
        job = state.submit("cancel", 8)
        try:
            assert entered.wait(3)
            job.cancelled.set()
            assert state.metrics() == (1, 0, 0)
            release.set()
            assert syncing.wait(3)
            assert state.metrics() == (1, 0, 0)
        finally:
            release.set()
            release_sync.set()
        until(lambda: state.metrics() == (0, 0, 1))
        assert job.output.empty()


def test_cancelled_waiting_request_never_reaches_backend():
    entered, release = threading.Event(), threading.Event()

    def produce(_prompt, _tokens):
        entered.set()
        assert release.wait(3)
        yield response()

    backend = SyntheticBackend(produce)
    with generations(backend) as state:
        first = state.submit("first", 8)
        try:
            assert entered.wait(3)
            cancelled = state.submit("cancelled", 8)
            cancelled.cancelled.set()
        finally:
            release.set()
        drain(first)
        until(lambda: state.metrics() == (0, 0, 0))
        assert backend.calls == [("first", 8)]


def test_output_backpressure_is_bounded_and_cancellation_unblocks_cleanup():
    closed = threading.Event()

    def produce(_prompt, _tokens):
        try:
            for count in range(1, 101):
                yield response("x", finish="length" if count == 100 else None, count=count)
        finally:
            closed.set()

    with generations(SyntheticBackend(produce)) as state:
        job = state.submit("slow reader", 100)
        until(job.output.full)
        assert job.output.qsize() == 16 and state.metrics() == (1, 0, 0)
        job.cancelled.set()
        assert closed.wait(3)
        until(lambda: state.metrics() == (0, 0, 1))


@pytest.mark.parametrize(
    "items",
    [
        (),
        (response(finish=None),),
        (response(count=99),),
        (response(count=True),),
        (response(finish="tool_calls"),),
        (response(), response(count=2)),
        (response(text="x" * (mlx_server.MAX_OUTPUT_BYTES + 1)),),
    ],
)
def test_invalid_or_unbounded_backend_output_is_poisoned(items):
    with generations(SyntheticBackend(lambda _p, _n: iter(items))) as state:
        assert drain(state.submit("bad", 8))[-1] == ("error", None)
        assert state.metrics()[2] == 1


@pytest.mark.parametrize(
    "key,value",
    [
        ("model", "remote/model"),
        ("max_tokens", True),
        ("max_tokens", 8193),
        ("temperature", True),
        ("temperature", 0.5),
        ("stream", False),
        ("stream_options", {"include_usage": False}),
        ("tools", []),
        ("messages", [{"role": "system", "content": "hi"}]),
        ("messages", [{"role": "user", "content": [{"type": "image"}]}]),
        ("messages", [{"role": "user", "content": "x" * 65537}]),
    ],
)
def test_reject_unsupported_request_before_queueing(key, value):
    body = payload()
    body[key] = value
    with pytest.raises(ValueError, match="invalid_request"):
        mlx_server.validate_request(body, "synthetic")


@contextmanager
def server(backend=None, key=None):
    instance = mlx_server.DiagnosticServer(
        ("127.0.0.1", 0), backend or SyntheticBackend(), "synthetic", key
    )
    thread = threading.Thread(target=instance.serve_forever, daemon=True)
    thread.start()
    try:
        yield instance
    finally:
        instance.shutdown()
        instance.server_close()
        thread.join(timeout=2)


def request(instance, method, path, body=None, headers=None):
    client = http.client.HTTPConnection(*instance.server_address, timeout=3)
    raw = json.dumps(body) if body is not None else None
    supplied = {"Content-Type": "application/json", **(headers or {})}
    try:
        client.request(method, path, body=raw, headers=supplied)
        response = client.getresponse()
        return response.status, response.read()
    finally:
        client.close()


def test_http_sse_identity_usage_and_readonly_endpoints():
    with server() as instance:
        status, raw = request(instance, "GET", "/version")
        assert status == 200
        details = json.loads(raw)
        assert details["synthetic"] is True
        assert details["engine_fit_protocol"] == "mlx-lm.v1"
        assert details["engine_fit_source_sha256"] == instance.source_hash
        assert request(instance, "GET", "/v1/models")[0] == 200
        status, raw = request(instance, "POST", "/v1/chat/completions", payload())
        assert status == 200 and raw.endswith(b"data: [DONE]\n\n")
        chunks = [json.loads(line[6:]) for line in raw.splitlines() if line.startswith(b"data: {")]
        assert chunks[-2]["choices"][0]["finish_reason"] == "stop"
        assert chunks[-1]["usage"] == {
            "prompt_tokens": 7,
            "completion_tokens": 1,
            "total_tokens": 8,
        }
        assert b"engine_fit:num_requests_running 0" in request(instance, "GET", "/metrics")[1]


def test_authentication_failure_never_enters_generation_queue():
    backend = SyntheticBackend()
    with server(backend, key="secret") as instance:
        assert request(instance, "POST", "/v1/chat/completions", payload())[0] == 401
        assert request(instance, "GET", "/metrics")[0] == 401
        assert backend.calls == [] and instance.generations.metrics() == (0, 0, 0)
        assert (
            request(instance, "GET", "/metrics", headers={"Authorization": "Bearer secret"})[0]
            == 200
        )


def test_metrics_remain_available_during_blocked_gpu_work():
    entered, release = threading.Event(), threading.Event()

    def produce(_prompt, _tokens):
        entered.set()
        assert release.wait(3)
        yield response()

    with server(SyntheticBackend(produce)) as instance:
        job = instance.generations.submit("blocked", 8)
        try:
            assert entered.wait(3)
            status, raw = request(instance, "GET", "/metrics")
            assert status == 200 and b"engine_fit:num_requests_running 1" in raw
        finally:
            release.set()
        drain(job)


def test_tcp_disconnect_keeps_running_until_backend_cleanup_and_sync():
    entered, release, syncing, release_sync = (threading.Event() for _ in range(4))

    def produce(_prompt, _tokens):
        entered.set()
        assert release.wait(3)
        yield response()

    def synchronize():
        syncing.set()
        assert release_sync.wait(3)

    with server(SyntheticBackend(produce, synchronize)) as instance:
        connection = socket.create_connection(instance.server_address, timeout=3)
        body = json.dumps(payload()).encode()
        connection.sendall(
            b"POST /v1/chat/completions HTTP/1.1\r\nHost: localhost\r\n"
            b"Content-Type: application/json\r\nContent-Length: "
            + str(len(body)).encode()
            + b"\r\n\r\n"
            + body
        )
        try:
            assert b"200" in connection.recv(4096)
            assert entered.wait(3)
            connection.setsockopt(socket.SOL_SOCKET, socket.SO_LINGER, struct.pack("ii", 1, 0))
            connection.close()
            until(lambda: instance.generations.active.cancelled.is_set())
            assert instance.generations.metrics() == (1, 0, 0)
            release.set()
            assert syncing.wait(3)
            assert instance.generations.metrics() == (1, 0, 0)
        finally:
            connection.close()
            release.set()
            release_sync.set()
        until(lambda: instance.generations.metrics() == (0, 0, 1))


def test_invalid_http_json_and_payload_never_enter_backend():
    backend = SyntheticBackend()
    with server(backend) as instance:
        body = copy.deepcopy(payload())
        body["draft_model"] = "remote/draft"
        assert request(instance, "POST", "/v1/chat/completions", body)[0] == 400
        assert request(instance, "POST", "/v1/completions", payload())[0] == 404
        client = http.client.HTTPConnection(*instance.server_address, timeout=3)
        client.request(
            "POST",
            "/v1/chat/completions",
            '{"model":1,"model":2}',
            {"Content-Type": "application/json"},
        )
        result = client.getresponse()
        assert result.status == 400
        result.read()
        client.close()
        assert backend.calls == []


def test_nonlocal_model_fails_before_importing_mlx(tmp_path):
    with pytest.raises(ValueError, match="local_model_directory_required"):
        mlx_server.MLXBackend(tmp_path / "nonexistent-model")


def test_server_rejects_nonloopback():
    with pytest.raises(ValueError, match="loopback_required"):
        mlx_server.DiagnosticServer(("0.0.0.0", 0), SyntheticBackend(), "synthetic")


def test_script_retains_python312_grammar_and_standalone_imports():
    tree = ast.parse(SCRIPT.read_text(), feature_version=(3, 12))
    for node in ast.walk(tree):
        if isinstance(node, ast.ImportFrom):
            assert not (node.module or "").startswith("inferyard")


def test_public_backend_is_offline_local_only_and_tracks_its_gpu_stream(tmp_path, monkeypatch):
    calls, stream = [], object()
    tokenizer = SimpleNamespace(apply_chat_template=lambda *_args, **_kwargs: [1, 2, 3])

    def load(path, **kwargs):
        calls.append(("load", path, kwargs))
        return "synthetic-model", tokenizer

    def generate(model, supplied_tokenizer, **kwargs):
        calls.append(("generate", model, supplied_tokenizer, kwargs))
        return iter((response(),))

    mx = SimpleNamespace(
        gpu="synthetic-gpu",
        new_thread_local_stream=lambda device: stream if device == "synthetic-gpu" else None,
        stream=lambda active: nullcontext(active),
        synchronize=lambda active: calls.append(("sync", active)),
    )
    monkeypatch.setitem(sys.modules, "mlx", SimpleNamespace(core=mx))
    monkeypatch.setitem(sys.modules, "mlx.core", mx)
    monkeypatch.setitem(sys.modules, "mlx_lm", SimpleNamespace(load=load, stream_generate=generate))
    monkeypatch.setitem(
        sys.modules, "mlx_lm.sample_utils", SimpleNamespace(make_sampler=lambda **kwargs: kwargs)
    )
    monkeypatch.setattr(mlx_server, "version", lambda package: "0.0.0+synthetic")
    monkeypatch.setenv("HF_HUB_OFFLINE", "0")
    monkeypatch.setenv("TRANSFORMERS_OFFLINE", "0")
    backend = mlx_server.MLXBackend(tmp_path)
    assert calls[0] == (
        "load",
        str(tmp_path.resolve()),
        {
            "tokenizer_config": {"trust_remote_code": False, "local_files_only": True},
            "trust_remote_code": False,
            "lazy": False,
        },
    )
    assert calls[1] == ("sync", stream)
    assert mlx_server.os.environ["HF_HUB_OFFLINE"] == "1"
    assert mlx_server.os.environ["TRANSFORMERS_OFFLINE"] == "1"
    assert len(list(backend.responses("text", 8))) == 1
    assert calls[-1] == (
        "generate",
        "synthetic-model",
        tokenizer,
        {"prompt": [1, 2, 3], "max_tokens": 8, "sampler": {"temp": 0}, "stream": stream},
    )
    backend.synchronize()
    assert calls[-1] == ("sync", stream)
