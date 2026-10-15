"""Discovery uses fixture process tables and HTTP transports, never model services."""

import json
from types import SimpleNamespace

import httpx
import jsonschema
import psutil
import pytest

from inferyard import cli
from inferyard.application import service_discovery as application
from inferyard.contracts.schemas import export_schema
from inferyard.platforms import service_discovery as platform


def process(pid=42, listeners=None):
    return {
        "pid": pid,
        "process_name": "arbitrary-engine-name",
        "process_start_ticks": 12345,
        "listeners": listeners or [("127.0.0.1", 8888)],
    }


def run(monkeypatch, capsys, processes, handler=None, limitations=()):
    monkeypatch.setattr(application, "collect_listeners", lambda: (processes, limitations))
    real_client = httpx.Client

    def client(**kwargs):
        assert kwargs == {"timeout": 2.0, "trust_env": False, "follow_redirects": False}
        assert handler is not None, "empty discovery must not open a client"
        return real_client(transport=httpx.MockTransport(handler), **kwargs)

    monkeypatch.setattr(application.httpx, "Client", client)
    code = cli.main(["service", "discover"])
    captured = capsys.readouterr()
    assert captured.err == ""
    result = json.loads(captured.out)
    jsonschema.validate(result, export_schema("service_discovery"))
    assert result["details"]["model_requests_sent"] == 0
    return code, result


def test_empty_discovery_is_complete(monkeypatch, capsys):
    code, result = run(monkeypatch, capsys, [])
    assert code == 0
    assert result["completeness"] == "complete"
    assert result["details"]["services"] == []


def test_models_and_multiple_owners_including_dual_stack(monkeypatch, capsys):
    calls = []

    def handler(request):
        calls.append(str(request.url))
        assert request.method == "GET"
        assert "authorization" not in request.headers
        assert "cookie" not in request.headers
        return httpx.Response(
            200,
            json={"data": [{"id": "local-model"}], "secret": "not-persisted"},
            headers={"x-secret": "not-persisted", "set-cookie": "secret=not-persisted"},
        )

    code, result = run(
        monkeypatch,
        capsys,
        [process(), process(43, [("::1", 8888), ("127.0.0.1", 8888)])],
        handler,
    )
    assert code == 0
    services = result["details"]["services"]
    assert [item["pid"] for item in services] == [42, 43]
    assert services[0]["process_name"] == "arbitrary-engine-name"
    for service in services:
        for endpoint in service["endpoints"]:
            assert endpoint["models"] == ["local-model"]
            assert endpoint["health"] == "2xx"
            assert endpoint["reason"] is None
            assert endpoint["ambiguous_listener"] is True
    assert len(calls) == 4
    assert "http://[::1]:8888/v1/models" in calls
    assert "not-persisted" not in json.dumps(result)


@pytest.mark.parametrize(
    "response, reason",
    [
        (httpx.Response(200, json={"data": []}), "models_empty"),
        (httpx.Response(200, json={}), "models_invalid_response"),
        (httpx.Response(200, json={"data": [{"id": 7}]}), "models_invalid_response"),
        (httpx.Response(200, content=b'{"data": [], "data": []}'), "models_invalid_response"),
        (httpx.Response(200, content=b"invalid json"), "models_invalid_response"),
        (httpx.Response(200, content=b"x" * (1024 * 1024 + 1)), "models_response_too_large"),
        (httpx.Response(401), "models_http_4xx"),
        (httpx.Response(503), "models_http_5xx"),
        (httpx.Response(302, headers={"location": "http://example.com"}), "models_http_3xx"),
        (httpx.ConnectError("secret"), "models_unreachable"),
        (httpx.ReadTimeout("secret"), "models_timeout"),
    ],
)
def test_probe_failure_is_null_with_fixed_reason(monkeypatch, capsys, response, reason):
    calls = []

    def handler(request):
        calls.append(request.url.path)
        if request.url.path == "/health":
            return httpx.Response(200)
        if isinstance(response, Exception):
            raise response
        return response

    code, result = run(monkeypatch, capsys, [process()], handler)
    assert code == 0
    endpoint = result["details"]["services"][0]["endpoints"][0]
    assert endpoint["models"] is None
    assert endpoint["reason"] == reason
    assert calls == ["/health", "/v1/models"]
    assert "secret" not in json.dumps(result)


def test_health_failure_does_not_prevent_models_probe(monkeypatch, capsys):
    def handler(request):
        if request.url.path == "/health":
            raise httpx.ConnectError("unreachable")
        return httpx.Response(200, json={"data": [{"id": "model"}]})

    code, result = run(monkeypatch, capsys, [process()], handler)
    assert code == 0
    endpoint = result["details"]["services"][0]["endpoints"][0]
    assert endpoint["health"] is None
    assert endpoint["models"] == ["model"]
    assert endpoint["reason"] == "health_unreachable"


def test_partial_collection_preserves_services(monkeypatch, capsys):
    code, result = run(
        monkeypatch,
        capsys,
        [process()],
        lambda _: httpx.Response(200, json={"data": [{"id": "model"}]}),
        ["listener_ownership_incomplete"],
    )
    assert code == 3
    assert result["completeness"] == "incomplete"
    assert result["limitations"] == ["listener_ownership_incomplete"]
    assert result["details"]["services"][0]["pid"] == 42


@pytest.mark.parametrize(
    "error, reason",
    [
        (platform.DiscoveryError("listener_enumeration_failed"), "listener_enumeration_failed"),
        (RuntimeError("secret"), "service_discovery_error"),
    ],
)
def test_tool_failure_is_code_four(monkeypatch, capsys, error, reason):
    def collect():
        raise error

    monkeypatch.setattr(application, "collect_listeners", collect)
    assert cli.main(["service", "discover"]) == 4
    result = json.loads(capsys.readouterr().out)
    jsonschema.validate(result, export_schema("service_discovery"))
    assert result["limitations"] == [reason]
    assert result["details"]["model_requests_sent"] == 0
    assert "secret" not in json.dumps(result)


def test_no_process_name_option(capsys):
    assert cli.main(["service", "discover", "--process-name", "llama-server"]) == 2
    assert json.loads(capsys.readouterr().out)["limitations"] == ["invalid_input"]


def proc_fixture(root, *, with_owner=True):
    (root / "net").mkdir()
    (root / "net/tcp").write_text(
        "header\n0: 0100007F:22B8 00000000:0000 0A 0 0 0 0 0 123\n"
        "1: 0100000A:22B9 00000000:0000 0A 0 0 0 0 0 456\n"
    )
    (root / "net/tcp6").write_text("header\n")
    if with_owner:
        directory = root / "42"
        (directory / "fd").mkdir(parents=True)
        (directory / "stat").write_text("42 (arbitrary engine) " + " ".join(["0"] * 19 + ["12345"]))
        (directory / "exe").symlink_to("/fixture/arbitrary-engine-name")
        (directory / "fd/3").symlink_to("socket:[123]")


def test_linux_proc_discovers_arbitrary_binary_and_excludes_nonloopback(tmp_path):
    proc_fixture(tmp_path)
    processes, limitations = platform.linux_listeners(tmp_path)
    assert processes == [process()]
    assert limitations == set()


def test_linux_missing_owner_is_partial(tmp_path):
    proc_fixture(tmp_path, with_owner=False)
    processes, limitations = platform.linux_listeners(tmp_path)
    assert processes == []
    assert limitations == {"listener_ownership_incomplete"}


def test_linux_unreadable_tables_are_tool_failure(tmp_path):
    with pytest.raises(platform.DiscoveryError, match="listener_enumeration_failed"):
        platform.linux_listeners(tmp_path)


@pytest.mark.parametrize("system", ["Darwin", "Windows"])
def test_psutil_permissions_and_identity_reuse(monkeypatch, system):
    monkeypatch.setattr(platform.platform, "system", lambda: system)
    monkeypatch.setattr(psutil, "pids", lambda: [42, 43])
    identities = []

    def identity(pid):
        identities.append(pid)
        return 12345

    def get_process(pid):
        if pid == 43:
            raise psutil.AccessDenied(pid)
        return SimpleNamespace(
            exe=lambda: "/fixture/arbitrary-engine-name",
            net_connections=lambda kind: [
                SimpleNamespace(
                    status=psutil.CONN_LISTEN, laddr=SimpleNamespace(ip="127.0.0.1", port=8888)
                )
            ],
        )

    monkeypatch.setattr(psutil, "Process", get_process)
    monkeypatch.setattr(platform.identity, "process_start_ticks", identity)
    processes, limitations = platform.collect_listeners()
    assert processes == [process()]
    assert limitations == {"listener_ownership_incomplete"}
    assert identities == [42, 42]


def test_changed_identity_is_not_attributed(monkeypatch):
    monkeypatch.setattr(psutil, "pids", lambda: [42])
    ticks = iter([12345, 12346])
    monkeypatch.setattr(platform.identity, "process_start_ticks", lambda _: next(ticks))
    monkeypatch.setattr(
        psutil,
        "Process",
        lambda _: SimpleNamespace(
            exe=lambda: "/fixture/engine",
            net_connections=lambda kind: [
                SimpleNamespace(
                    status=psutil.CONN_LISTEN, laddr=SimpleNamespace(ip="::", port=8888)
                )
            ],
        ),
    )
    assert platform.psutil_listeners() == ([], {"process_identity_unavailable"})


@pytest.mark.parametrize(
    "address, expected",
    [
        ("0.0.0.0", "127.0.0.1"),
        ("::", "::1"),
        ("::1", "::1"),
        ("127.0.0.2", "127.0.0.2"),
        ("::ffff:127.0.0.1", "::ffff:127.0.0.1"),
        ("192.168.1.1", None),
    ],
)
def test_only_loopback_probe_targets(address, expected):
    assert platform.loopback_host(address) == expected
