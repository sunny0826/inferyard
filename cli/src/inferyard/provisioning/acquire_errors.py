"""Sanitized failure classification helpers for model and engine acquisition."""

import errno

import httpx


def classify_http_status(status_code: int) -> str:
    """Classify a non-200 HTTP response status without leaking URLs or bodies."""
    if status_code in (401, 403):
        return "auth_required"
    if status_code == 404:
        return "not_found"
    if status_code == 429:
        return "rate_limited"
    return "upstream_http_error"


def classify_httpx_error(exc: Exception) -> str:
    """Classify an httpx transport or protocol exception into a fixed cause."""
    if isinstance(exc, httpx.TimeoutException):
        return "network_timeout"
    if isinstance(exc, (httpx.ConnectError, httpx.NetworkError, httpx.RemoteProtocolError)):
        return "connection_error"
    return "transfer_error"


def classify_os_error(exc: OSError) -> str:
    """Classify a local filesystem exception without exposing OS paths or messages."""
    if exc.errno == errno.ENOSPC:
        return "disk_full"
    if isinstance(exc, PermissionError) or exc.errno in (errno.EACCES, errno.EPERM):
        return "permission_denied"
    return "io_error"


def failure_details(stage: str, cause: str) -> dict[str, str]:
    """Build sanitized failure metadata safe for stderr and CommandResult.details."""
    return {"failure_stage": stage, "failure_cause": cause}
