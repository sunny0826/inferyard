"""Discovery uses fixture process tables and HTTP transports, never model services."""

import json

import httpx
import jsonschema
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
    assert "http://[::1]:8888/v1/models" in calls
    assert "not-persisted" not in json.dumps(result)


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
