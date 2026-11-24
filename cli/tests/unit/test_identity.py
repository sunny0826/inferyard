import pytest

from inferyard.adapters.prism import PrismAdapter
from inferyard.platforms import identity


def test_adapter_rechecks_dns_after_listener_resolution(monkeypatch):
    calls = iter(("127.0.0.1", "192.0.2.1"))
    monkeypatch.setattr(
        identity.socket,
        "getaddrinfo",
        lambda *a, **k: [(None, None, None, None, (next(calls), 80))],
    )
    origin, address, _ = identity.service_origin("http://localhost")
    assert origin == "http://localhost"
    assert address == "127.0.0.1"
    with pytest.raises(identity.PreflightError, match="localhost_resolved_outside_loopback"):
        PrismAdapter(origin)
