"""Unit tests for model resolve, explicit proxy origin, and cross-origin auth stripping."""

import hashlib
import json
import subprocess
import sys
from types import SimpleNamespace

import httpx
import pytest

from inferyard.application import engine_prepare as app_engine_prepare
from inferyard.application import engine_source as app_engine_source
from inferyard.application import model_resolve as app_model_resolve
from inferyard.application import model_source as app_model_source
from inferyard.application.dispatch import default_backend
from inferyard.cli import main
from inferyard.cli.arguments import parser
from inferyard.cli.request import build_request
from inferyard.provisioning import (
    engine_prepare,
    engine_source_download,
    http_client,
    model_resolve,
    model_source,
    model_source_download,
)
from inferyard.provisioning.preparation_io import PreparationError

SHA_HF = "23749fefcc72300e3a2ad315e1317431b06b590a"
SHA_MS = "6abe20cd0aed577f4d0b267935868ecae190aee9"
SECRET = "SECRET_RESOLVE_TOKEN_999"
REPO = "Qwen/Qwen3-0.6B-GGUF"
GGUF = "Qwen3-0.6B-Q8_0.gguf"
HF = f"https://huggingface.co/{REPO}"
MS = f"https://modelscope.cn/{REPO}"
PROXY = "http://127.0.0.1:7890"


@pytest.mark.parametrize(
    ("raw", "expected"),
    [
        (None, None),
        (PROXY, PROXY),
        (f"{PROXY}/", PROXY),
        ("HTTPS://Proxy.Example.com/", "https://proxy.example.com"),
        ("http://[::1]:8080/", "http://[::1]:8080"),
        ("https://proxy.internal", "https://proxy.internal"),
    ],
)
def test_validate_proxy_origin_accepts_and_normalizes(raw, expected):
    assert http_client.validate_proxy_origin(raw) == expected


@pytest.mark.parametrize(
    "raw",
    [
        "",
        123,
        f" {PROXY}",
        f"{PROXY}\n",
        "http://p roxy:7890",
        "http://代理:7890",
        "socks5://127.0.0.1:1080",
        "ftp://127.0.0.1:21",
        "http://user:pass@127.0.0.1:7890",
        "http://@127.0.0.1:7890",
        f"{PROXY}/path",
        f"{PROXY}//",
        f"{PROXY}?a=1",
        f"{PROXY}?",
        f"{PROXY}#frag",
        f"{PROXY}#",
        "http://127.0.0.1:0",
        "http://127.0.0.1:65536",
        "http://127.0.0.1:abc",
        "http://127.0.0.1:",
        "http://",
        "http:///",
    ],
)
def test_validate_proxy_origin_rejects_invalid_values(raw):
    with pytest.raises(PreparationError) as exc_info:
        http_client.validate_proxy_origin(raw)
    assert exc_info.value.reason == "invalid_proxy_origin"
    assert exc_info.value.details == {
        "failure_stage": "proxy",
        "failure_cause": "invalid_proxy_origin",
    }


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


@pytest.mark.parametrize(
    ("raw_url", "platform", "repo", "ref", "path"),
    [
        (f"{HF}/resolve/main/{GGUF}?download=true", "huggingface", REPO, "main", GGUF),
        (
            f"https://huggingface.co/ggml-org/models-moved/blob/{SHA_HF.upper()}/"
            "tinyllamas/stories260K.gguf?download=1",
            "huggingface",
            "ggml-org/models-moved",
            SHA_HF.upper(),
            "tinyllamas/stories260K.gguf",
        ),
        (f"{MS}/resolve/master/{GGUF}", "modelscope", REPO, "master", GGUF),
        (
            f"https://modelscope.cn/models/{REPO}/file/view/master/sub/model.gguf?download=true",
            "modelscope",
            REPO,
            "master",
            "sub/model.gguf",
        ),
        (f"https://modelscope.cn/models/{REPO}/blob/v1.0/{GGUF}", "modelscope", REPO, "v1.0", GGUF),
    ],
)
def test_parse_resolvable_source_valid(raw_url, platform, repo, ref, path):
    parsed = model_resolve.parse_resolvable_source(raw_url)
    assert (parsed.platform, parsed.repository, parsed.ref, parsed.path) == (
        platform,
        repo,
        ref,
        path,
    )


@pytest.mark.parametrize(
    "bad_url",
    [
        f"http://huggingface.co/{REPO}/resolve/main/a.gguf",
        f"https://example.invalid/{REPO}/resolve/main/a.gguf",
        f"https://SECRET@huggingface.co/{REPO}/resolve/main/a.gguf",
        f"{HF}/tree/main/a.gguf",
        f"{HF}/resolve/main/a.bin",
        f"{HF}/resolve/main/.gguf",
        f"{HF}/resolve/..%2fmain/a.gguf",
        f"{HF}/resolve/main%2fsub/a.gguf",
        f"{HF}/resolve/main/../a.gguf",
        f"{HF}/resolve/main/a.gguf?token=SECRET",
        f"{HF}/resolve/main/a.gguf?download=false",
        f"{HF}/resolve/main/a.gguf?",
        f"{HF}/resolve/main/a.gguf#SECRET",
        f"https://modelscope.cn/models/{REPO}/other/view/master/a.gguf",
    ],
)
def test_parse_resolvable_source_rejects_invalid(bad_url):
    with pytest.raises(PreparationError) as exc_info:
        model_resolve.parse_resolvable_source(bad_url)
    assert exc_info.value.reason == "invalid_model_source"


def test_resolve_model_url_huggingface_and_already_pinned(monkeypatch):
    monkeypatch.setenv("TEST_HF_TOK", SECRET)
    seen: list[tuple[str, str, str | None]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append((request.method, str(request.url), request.headers.get("authorization")))
        if request.url.path == f"/api/models/{REPO}/revision/main":
            return httpx.Response(200, json={"sha": SHA_HF.upper()})
        if request.method == "HEAD" and request.url.host == "huggingface.co":
            return httpx.Response(302, headers={"Location": "https://us.aws.cdn.hf.co/blob"})
        if request.method == "HEAD" and request.url.host == "us.aws.cdn.hf.co":
            return httpx.Response(200, headers={"Content-Length": "639446688"})
        return httpx.Response(404)

    monkeypatch.setattr(
        model_resolve,
        "open_http_client",
        lambda timeout_seconds, **kw: http_client.open_http_client(
            timeout_seconds, transport=httpx.MockTransport(handler), **kw
        ),
    )

    res = model_resolve.resolve_model_url(
        f"{HF}/resolve/main/{GGUF}?download=true",
        token_env="TEST_HF_TOK",
    )
    pinned = f"{HF}/blob/{SHA_HF}/{GGUF}"
    dl_url = f"{HF}/resolve/{SHA_HF}/{GGUF}"
    assert res == {
        "definition": "model-resolve.v1",
        "platform": "huggingface",
        "repository": REPO,
        "revision": SHA_HF,
        "path": GGUF,
        "pinned_url": pinned,
        "download_url": dl_url,
        "filename": GGUF,
        "bytes": 639446688,
        "already_pinned": False,
        "model_requests_sent": 0,
    }
    assert model_source.parse_source(res["pinned_url"]).revision == SHA_HF
    assert seen == [
        ("GET", f"https://huggingface.co/api/models/{REPO}/revision/main", f"Bearer {SECRET}"),
        ("HEAD", dl_url, f"Bearer {SECRET}"),
        ("HEAD", "https://us.aws.cdn.hf.co/blob", None),
    ]

    seen.clear()
    pinned_res = model_resolve.resolve_model_url(res["pinned_url"])
    assert pinned_res["already_pinned"] is True
    assert pinned_res["bytes"] == 639446688
    assert len(seen) == 2


def test_resolve_model_url_modelscope_and_head_405_fallback():
    calls: list[tuple[str, str]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        calls.append((request.method, str(request.url)))
        if request.url.path == f"/api/v1/models/{REPO}/repo/files":
            return httpx.Response(
                200,
                json={
                    "Code": 200,
                    "Success": True,
                    "Data": {
                        "Files": [
                            {"Path": "README.md", "Revision": "0" * 40},
                            {"Path": GGUF, "Revision": SHA_MS},
                        ]
                    },
                },
            )
        if request.method == "HEAD":
            return httpx.Response(405)
        return httpx.Response(404)

    ms_view = f"https://modelscope.cn/models/{REPO}/file/view/master/{GGUF}?download=true"
    with httpx.Client(transport=httpx.MockTransport(handler)) as client:
        res = model_resolve.resolve_model_url(ms_view, client=client)
    assert res["platform"] == "modelscope"
    assert res["revision"] == SHA_MS
    assert res["pinned_url"] == f"{MS}/blob/{SHA_MS}/{GGUF}"
    assert res["download_url"] == (
        f"https://modelscope.cn/api/v1/models/{REPO}/repo?Revision={SHA_MS}&FilePath={GGUF}"
    )
    assert res["bytes"] is None
    assert res["already_pinned"] is False
    assert model_source.parse_source(res["pinned_url"]).download_url == res["download_url"]
    assert len(calls) == 2


def test_resolve_model_url_failures_and_token_safety(monkeypatch):
    monkeypatch.delenv("MISSING_RESOLVE_TOKEN", raising=False)
    with pytest.raises(PreparationError) as tok_exc:
        model_resolve.resolve_model_url(
            f"{HF}/resolve/main/{GGUF}",
            token_env="MISSING_RESOLVE_TOKEN",
        )
    assert tok_exc.value.reason == "model_resolve_failed"
    assert tok_exc.value.details == {"failure_stage": "token", "failure_cause": "token_env_missing"}

    for status, stage, cause in [
        (401, "resolve", "auth_required"),
        (404, "resolve", "not_found"),
        (429, "resolve", "rate_limited"),
    ]:
        client = httpx.Client(transport=httpx.MockTransport(lambda _r, s=status: httpx.Response(s)))
        with client, pytest.raises(PreparationError) as exc:
            model_resolve.resolve_model_url(f"{HF}/resolve/main/{GGUF}", client=client)
        assert exc.value.reason == "model_resolve_failed"
        assert exc.value.details == {"failure_stage": stage, "failure_cause": cause}

    client_403 = httpx.Client(transport=httpx.MockTransport(lambda _r: httpx.Response(403)))
    with client_403, pytest.raises(PreparationError) as head_exc:
        model_resolve.resolve_model_url(f"{HF}/blob/{SHA_HF}/{GGUF}", client=client_403)
    assert head_exc.value.details == {"failure_stage": "probe", "failure_cause": "auth_required"}

    timeout_client = httpx.Client(
        transport=httpx.MockTransport(
            lambda _r: (_ for _ in ()).throw(httpx.ReadTimeout("https://secret.example/timeout"))
        )
    )
    with timeout_client, pytest.raises(PreparationError) as net_exc:
        model_resolve.resolve_model_url(f"{HF}/blob/{SHA_HF}/{GGUF}", client=timeout_client)
    assert net_exc.value.details == {"failure_stage": "resolve", "failure_cause": "network_timeout"}


def test_model_acquire_redirect_strips_auth_via_open_http_client(monkeypatch, tmp_path):
    monkeypatch.setenv("HF_ACQ_SECRET", SECRET)
    seen: list[tuple[str, str | None]] = []
    payload = b"gguf-bytes"

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append((str(request.url), request.headers.get("authorization")))
        if request.url.host == "huggingface.co":
            return httpx.Response(302, headers={"Location": "https://cdn-lfs.huggingface.co/m.gguf"})
        return httpx.Response(
            200,
            headers={"Content-Encoding": "identity", "Content-Length": str(len(payload))},
            stream=httpx.ByteStream(payload),
        )

    captured_proxy: list[str | None] = []

    def fake_open(timeout_seconds, **kw):
        captured_proxy.append(kw.pop("proxy_origin", None))
        return http_client.open_http_client(
            timeout_seconds, transport=httpx.MockTransport(handler), **kw
        )

    monkeypatch.setattr(model_source_download, "open_http_client", fake_open)

    details = model_source_download.acquire(
        f"{HF}/blob/{SHA_HF}/{GGUF}",
        tmp_path / "dl",
        token_env="HF_ACQ_SECRET",
        proxy_origin=f"{PROXY}/",
    )
    assert captured_proxy == [f"{PROXY}/"]
    assert details["sha256"] == hashlib.sha256(payload).hexdigest()
    assert seen == [
        (f"{HF}/resolve/{SHA_HF}/{GGUF}", f"Bearer {SECRET}"),
        ("https://cdn-lfs.huggingface.co/m.gguf", None),
    ]


def test_cli_flags_dispatch_and_invalid_proxy_origin(monkeypatch, tmp_path, capsys):
    p = parser()
    proxy = f"{PROXY}/"
    model_url = f"{HF}/resolve/main/{GGUF}"
    pinned = f"{HF}/blob/{SHA_HF}/{GGUF}"
    engine_url = "https://example.test/llama.tar.gz"

    for argv, cmd in [
        (["model", "resolve", "--source", model_url, "--proxy-origin", proxy], "model resolve"),
        (["model", "acquire", "--source", pinned, "--out", str(tmp_path / "m"), "--proxy-origin", proxy], "model acquire"),
        (["engine", "acquire", "--source", engine_url, "--out", str(tmp_path / "e"), "--proxy-origin", proxy], "engine acquire"),
        (["engine", "prepare", "--source", engine_url, "--out", str(tmp_path / "p"), "--proxy-origin", proxy], "engine prepare"),
    ]:
        req = build_request(p.parse_args(argv))
        assert (req.command, req.proxy_origin) == (cmd, proxy)

    seen = {}
    monkeypatch.setattr(model_resolve, "resolve_model_url", lambda *a, **k: seen.setdefault("r", k) or {"definition": "model-resolve.v1"})
    monkeypatch.setattr(model_source_download, "acquire", lambda *a, **k: seen.setdefault("m", k) or {"bytes": 1})
    monkeypatch.setattr(engine_source_download, "acquire", lambda *a, **k: seen.setdefault("e", k) or {"bytes": 1})
    monkeypatch.setattr(engine_prepare, "prepare", lambda *a, **k: seen.setdefault("p", k) or {"bytes": 1})

    code, res = default_backend(build_request(p.parse_args(["model", "resolve", "--source", model_url, "--proxy-origin", proxy])))
    assert (code, res.status, res.completeness) == (0, "resolved", "complete")
    app_model_source.execute(SimpleNamespace(command="model acquire", model_source_url=pinned, out=tmp_path / "m", model_token_env=None, model_timeout_seconds=10.0, proxy_origin=proxy))
    app_engine_source.execute(SimpleNamespace(command="engine acquire", engine_source_url=engine_url, out=tmp_path / "e", engine_timeout_seconds=10.0, proxy_origin=proxy))
    app_engine_prepare.execute(SimpleNamespace(command="engine prepare", engine_source_url=engine_url, out=tmp_path / "p", engine_timeout_seconds=10.0, proxy_origin=proxy))
    assert all(seen[k]["proxy_origin"] == proxy for k in ("r", "m", "e", "p"))
    monkeypatch.undo()

    bad_code = main(["model", "resolve", "--source", model_url, "--proxy-origin", "http://user:pass@127.0.0.1:7890"])
    captured = capsys.readouterr()
    assert bad_code == 2
    payload = json.loads(captured.out)
    assert payload["limitations"] == ["invalid_proxy_origin"]
    assert payload["details"] == {"failure_stage": "proxy", "failure_cause": "invalid_proxy_origin"}
    assert "user:pass" not in captured.out + captured.err
    assert app_model_resolve is not None


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
    proc = subprocess.run([sys.executable, "-c", script, cli_root], capture_output=True, text=True, check=False)
    assert proc.returncode == 0, (proc.stdout, proc.stderr)
