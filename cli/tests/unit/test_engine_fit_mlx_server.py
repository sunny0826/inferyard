"""Synthetic MLX protocol/lifecycle checks; no MLX import or real model execution."""

import ast
import copy
import http.client
import importlib.util
import json
import queue
import socket
import struct
import threading
import time
from contextlib import contextmanager
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


def test_script_retains_python312_grammar():
    ast.parse(SCRIPT.read_text(), feature_version=(3, 12))
