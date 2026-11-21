"""Scoped HTTP client factory with explicit proxy-origin and cross-origin auth stripping."""

from __future__ import annotations

from collections.abc import Callable
import math
from urllib.parse import urlsplit

import httpx

from inferyard.provisioning.acquire_errors import failure_details
from inferyard.provisioning.preparation_io import PreparationError

Origin = tuple[str, str, int | None]


def _invalid_proxy() -> PreparationError:
    return PreparationError(
        "invalid_proxy_origin",
        details=failure_details("proxy", "invalid_proxy_origin"),
    )


def validate_proxy_origin(proxy_origin: str | None) -> str | None:
    """Validate and normalize an explicit http(s) proxy origin without credentials or path."""
    if proxy_origin is None:
        return None
    if (
        not isinstance(proxy_origin, str)
        or not proxy_origin
        or not proxy_origin.isascii()
        or any(ord(ch) < 33 or ord(ch) == 127 for ch in proxy_origin)
        or "?" in proxy_origin
        or "#" in proxy_origin
    ):
        raise _invalid_proxy()
    try:
        parsed = urlsplit(proxy_origin)
        scheme = parsed.scheme.lower()
        if scheme not in ("http", "https"):
            raise ValueError
        if (
            parsed.username is not None
            or parsed.password is not None
            or "@" in parsed.netloc
            or not parsed.netloc
            or parsed.netloc.endswith(":")
            or parsed.path not in ("", "/")
            or parsed.query != ""
            or parsed.fragment != ""
        ):
            raise ValueError
        host = parsed.hostname
        if not host:
            raise ValueError
        port = parsed.port
        if port is not None and not 1 <= port <= 65535:
            raise ValueError
    except ValueError:
        raise _invalid_proxy() from None
    host_lower = host.lower()
    formatted_host = f"[{host_lower}]" if ":" in host_lower else host_lower
    netloc = f"{formatted_host}:{port}" if port is not None else formatted_host
    return f"{scheme}://{netloc}"


def origin_of(url: str | httpx.URL) -> Origin:
    """Return canonical (scheme, lowercase_host, effective_port) for one HTTP(S) URL."""
    if isinstance(url, httpx.URL):
        scheme = (url.scheme or "").lower()
        host = (url.host or "").lower()
        port = url.port
    else:
        parsed = urlsplit(url)
        scheme = parsed.scheme.lower()
        host = (parsed.hostname or "").lower()
        port = parsed.port
    if port is None:
        if scheme == "http":
            port = 80
        elif scheme == "https":
            port = 443
    return scheme, host, port


def _strip_cross_origin_auth(allowed_origin: Origin) -> Callable[[httpx.Request], None]:
    def _hook(request: httpx.Request) -> None:
        if origin_of(request.url) != allowed_origin:
            for key in [name for name in request.headers if name.lower() == "authorization"]:
                del request.headers[key]

    return _hook


def _default_timeout(seconds: int | float | httpx.Timeout) -> httpx.Timeout:
    if isinstance(seconds, httpx.Timeout):
        return seconds
    try:
        valid = type(seconds) in (int, float) and math.isfinite(seconds) and seconds > 0
    except OverflowError:
        valid = False
    if not valid:
        raise PreparationError("invalid_input")
    return httpx.Timeout(seconds)


def open_http_client(
    timeout_seconds: int | float | httpx.Timeout,
    *,
    proxy_origin: str | None = None,
    auth_origin: Origin | str | httpx.URL | None = None,
    timeout_factory: Callable[[int | float], httpx.Timeout] | None = None,
    transport: httpx.BaseTransport | None = None,
) -> httpx.Client:
    """Build an httpx.Client with trust_env=False, validated proxy, and auth origin hook."""
    validated_proxy = validate_proxy_origin(proxy_origin)
    timeout = (
        timeout_factory(timeout_seconds)
        if timeout_factory is not None
        else _default_timeout(timeout_seconds)
    )
    kwargs: dict[str, object] = {
        "trust_env": False,
        "follow_redirects": True,
        "timeout": timeout,
    }
    if validated_proxy is not None:
        kwargs["proxy"] = validated_proxy
    if auth_origin is not None:
        allowed = auth_origin if isinstance(auth_origin, tuple) else origin_of(auth_origin)
        kwargs["event_hooks"] = {"request": [_strip_cross_origin_auth(allowed)]}
    if transport is not None:
        kwargs["transport"] = transport
    return httpx.Client(**kwargs)
