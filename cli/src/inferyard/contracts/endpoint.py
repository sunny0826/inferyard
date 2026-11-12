"""Loopback endpoint syntax shared by configuration and host identity checks."""

import ipaddress
from urllib.parse import urlsplit

from inferyard.errors import ContractError


def validate_endpoint(url: str) -> None:
    try:
        parts = urlsplit(url)
        host = parts.hostname
        port = parts.port
        if (
            parts.scheme not in ("http", "https")
            or host is None
            or parts.username is not None
            or parts.password is not None
            or parts.query
            or parts.fragment
            or "?" in url
            or "#" in url
            or any(character.isspace() for character in url)
            or parts.path not in ("", "/")
            or port == 0
        ):
            raise ValueError
        # A future transport must pin localhost resolution to loopback too.
        if host != "localhost" and not ipaddress.ip_address(host).is_loopback:
            raise ValueError
    except ValueError as exc:
        raise ContractError(
            "config.endpoint.url", "requires a loopback origin without credentials"
        ) from exc
