"""Packaged MLX server Python 3.12 compatibility and shared synthetic test helpers."""

import ast
import http.client
import json
import time
from pathlib import Path
from types import SimpleNamespace

SCRIPT = Path(__file__).parents[2] / "src/inferyard/data/engine_fit/mlx_server.py"


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


def until(predicate):
    deadline = time.monotonic() + 3
    while not predicate():
        if time.monotonic() >= deadline:
            raise AssertionError("synthetic lifecycle did not reach expected state")
        time.sleep(0.005)


def request(instance, method, path, body=None, headers=None):
    client = http.client.HTTPConnection(*instance.server_address, timeout=3)
    raw = json.dumps(body) if body is not None else None
    supplied = {"Content-Type": "application/json", **(headers or {})}
    try:
        client.request(method, path, body=raw, headers=supplied)
        resp = client.getresponse()
        return resp.status, resp.read()
    finally:
        client.close()


def test_script_retains_python312_grammar():
    ast.parse(SCRIPT.read_text(), feature_version=(3, 12))
