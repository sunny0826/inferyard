"""Unit tests for cross-origin auth stripping and CLI help cold-start."""

import subprocess
import sys

import httpx

from inferyard.provisioning import http_client, model_resolve

SECRET = "SECRET_RESOLVE_TOKEN_999"


def test_open_http_client_ignores_env_proxies_and_strips_cross_origin_auth(monkeypatch):
    monkeypatch.setenv("HTTPS_PROXY", "socks5://127.0.0.1:9999")
    monkeypatch.setenv("ALL_PROXY", "socks5://127.0.0.1:9999")

    with http_client.open_http_client(15, proxy_origin="http://127.0.0.1:8888/") as proxied:
        assert proxied._mounts

    seen_auth: list[tuple[str, str | None]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen_auth.append((str(request.url), request.headers.get("authorization")))
        if request.url.host == "huggingface.co" and request.url.path == "/start":
            return httpx.Response(302, headers={"Location": "https://huggingface.co/same-origin"})
        if request.url.host == "huggingface.co" and request.url.path == "/same-origin":
            return httpx.Response(302, headers={"Location": "https://cdn-lfs.huggingface.co/blob"})
        return httpx.Response(200, content=b"ok")

    with http_client.open_http_client(
        15,
        auth_origin=http_client.origin_of("https://huggingface.co"),
        transport=httpx.MockTransport(handler),
    ) as client:
        resp = client.get(
            "https://huggingface.co/start",
            headers={"Authorization": f"Bearer {SECRET}"},
        )
    assert resp.status_code == 200
    assert seen_auth == [
        ("https://huggingface.co/start", f"Bearer {SECRET}"),
        ("https://huggingface.co/same-origin", f"Bearer {SECRET}"),
        ("https://cdn-lfs.huggingface.co/blob", None),
    ]

    port_calls: list[tuple[str, str | None]] = []

    def port_handler(request: httpx.Request) -> httpx.Response:
        port_calls.append((str(request.url), request.headers.get("authorization")))
        if request.url.port in (None, 443):
            return httpx.Response(302, headers={"Location": "https://huggingface.co:8443/alt"})
        return httpx.Response(200, content=b"ok")

    with http_client.open_http_client(
        15,
        auth_origin="https://huggingface.co",
        transport=httpx.MockTransport(port_handler),
    ) as client:
        client.get("https://huggingface.co/m", headers={"Authorization": f"Bearer {SECRET}"})
    assert port_calls == [
        ("https://huggingface.co/m", f"Bearer {SECRET}"),
        ("https://huggingface.co:8443/alt", None),
    ]


def test_model_resolve_and_acquire_help_cold_start():
    cli_root = model_resolve.__file__.rsplit("/src/", 1)[0]
    script = """
import contextlib, importlib.abc, io, sys
sys.path.insert(0, sys.argv[1])
BLOCKED = ("inferyard.runtime", "inferyard.extensions", "inferyard.adapters")
class F(importlib.abc.MetaPathFinder):
    def find_spec(self, name, path=None, target=None):
        if any(name == b or name.startswith(b + ".") for b in BLOCKED):
            raise AssertionError(name)
sys.meta_path.insert(0, F())
from inferyard.cli import main
for args in (
    ["model", "resolve", "--help"],
    ["model", "acquire", "--help"],
    ["engine", "acquire", "--help"],
    ["engine", "prepare", "--help"],
):
    with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
        try:
            main(args)
        except SystemExit as e:
            assert e.code in (0, None)
"""
    proc = subprocess.run(
        [sys.executable, "-c", script, cli_root],
        capture_output=True,
        text=True,
        check=False,
    )
    assert proc.returncode == 0, (proc.stdout, proc.stderr)
