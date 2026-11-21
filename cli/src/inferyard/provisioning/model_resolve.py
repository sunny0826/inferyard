"""Resolve HuggingFace and ModelScope GGUF URLs to pinned commit revisions."""

from __future__ import annotations

from dataclasses import dataclass
import math
import os
import re
from urllib.parse import parse_qsl, quote, unquote, urlencode, urlsplit

import httpx

from inferyard.contracts.validation import strict_json_loads
from inferyard.errors import ContractError
from inferyard.provisioning.acquire_errors import (
    classify_http_status,
    classify_httpx_error,
    failure_details,
)
from inferyard.provisioning.http_client import open_http_client, origin_of, validate_proxy_origin
from inferyard.provisioning.model_source import PLATFORMS, parse_source, validate_token_env
from inferyard.provisioning.preparation_io import PreparationError
from inferyard.provisioning.transfer_progress import parse_content_length

_SHA40 = re.compile(r"[0-9a-fA-F]{40}\Z")
_OWNER_REPO = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]*\Z")
_RESOLVE_FAILED = "model_resolve_failed"


@dataclass(frozen=True, slots=True)
class ResolvableModelSource:
    platform: str
    repository: str
    ref: str
    path: str

    @property
    def host(self) -> str:
        return "huggingface.co" if self.platform == "huggingface" else "modelscope.cn"


def _timeout(seconds: int | float) -> httpx.Timeout:
    try:
        valid = type(seconds) in (int, float) and math.isfinite(seconds) and seconds > 0
    except OverflowError:
        valid = False
    if not valid:
        raise PreparationError("invalid_model_source")
    return httpx.Timeout(seconds)


def _token(name: str | None) -> str | None:
    validate_token_env(name)
    if name is None:
        return None
    value = os.environ.get(name)
    if not value or not value.isascii() or any(ord(c) < 33 or ord(c) == 127 for c in value):
        raise PreparationError(
            _RESOLVE_FAILED,
            details=failure_details("token", "token_env_missing"),
        )
    return value


def _extract_segments(host: str, decoded: list[str]) -> tuple[str, str, str, list[str]]:
    if (
        host == "modelscope.cn"
        and len(decoded) >= 6
        and decoded[0] == "models"
        and (
            decoded[3] in ("blob", "resolve")
            or (len(decoded) >= 7 and decoded[3:5] == ["file", "view"])
        )
    ):
        owner, repo = decoded[1], decoded[2]
        if decoded[3] == "file":
            return owner, repo, decoded[5], decoded[6:]
        return owner, repo, decoded[4], decoded[5:]
    if len(decoded) < 5 or decoded[2] not in ("blob", "resolve"):
        raise ValueError
    return decoded[0], decoded[1], decoded[3], decoded[4:]


def parse_resolvable_source(raw_url: str) -> ResolvableModelSource:
    """Parse one HuggingFace or ModelScope GGUF URL with branch/tag or commit revision."""
    try:
        if (
            not isinstance(raw_url, str)
            or not raw_url.isascii()
            or any(ord(c) < 33 or ord(c) == 127 for c in raw_url)
            or "#" in raw_url
        ):
            raise ValueError
        parsed = urlsplit(raw_url)
        host = parsed.netloc.lower()
        if (
            parsed.scheme != "https"
            or parsed.username is not None
            or parsed.password is not None
            or "@" in parsed.netloc
            or host not in PLATFORMS
        ):
            raise ValueError
        if "?" in raw_url:
            query_items = parse_qsl(parsed.query, keep_blank_values=True)
            if query_items not in ([("download", "true")], [("download", "1")]):
                raise ValueError
        parts = parsed.path.split("/")
        if len(parts) < 6 or parts[0] != "":
            raise ValueError
        for part in parts[1:]:
            if re.search(r"%(?![0-9a-fA-F]{2})", part):
                raise ValueError
        decoded = [unquote(part, errors="strict") for part in parts[1:]]
        for part in decoded:
            if (
                part in ("", ".", "..")
                or not part.isascii()
                or any(ord(c) < 32 or ord(c) == 127 or c in "/\\:" for c in part)
                or part != part.strip()
            ):
                raise ValueError
        owner, repo, ref, file_parts = _extract_segments(host, decoded)
        if not _OWNER_REPO.fullmatch(owner) or not _OWNER_REPO.fullmatch(repo):
            raise ValueError
        if (
            not ref
            or ".." in ref
            or any(ord(c) < 33 or ord(c) == 127 or c in "/\\:" for c in ref)
        ):
            raise ValueError
        if not file_parts:
            raise ValueError
        file_path = "/".join(file_parts)
        if not file_path.endswith(".gguf") or len(file_parts[-1]) <= len(".gguf"):
            raise ValueError
        return ResolvableModelSource(PLATFORMS[host], f"{owner}/{repo}", ref, file_path)
    except ValueError, UnicodeError:
        raise PreparationError("invalid_model_source") from None


def _parse_json_object(response: httpx.Response, *, stage: str) -> dict:
    try:
        payload = strict_json_loads(response.text)
    except ContractError, ValueError:
        raise PreparationError(
            _RESOLVE_FAILED,
            details=failure_details(stage, "upstream_http_error"),
        ) from None
    if not isinstance(payload, dict):
        raise PreparationError(
            _RESOLVE_FAILED,
            details=failure_details(stage, "upstream_http_error"),
        )
    return payload


def _resolve_huggingface_revision(
    client: httpx.Client,
    parsed: ResolvableModelSource,
    headers: dict[str, str],
) -> str:
    url = (
        f"https://huggingface.co/api/models/{parsed.repository}/revision/"
        f"{quote(parsed.ref, safe='')}"
    )
    resp = client.get(url, headers=headers)
    if resp.status_code != 200:
        raise PreparationError(
            _RESOLVE_FAILED,
            details=failure_details("resolve", classify_http_status(resp.status_code)),
        )
    data = _parse_json_object(resp, stage="resolve")
    sha = data.get("sha")
    if not isinstance(sha, str) or _SHA40.fullmatch(sha) is None:
        raise PreparationError(
            _RESOLVE_FAILED,
            details=failure_details("resolve", "upstream_http_error"),
        )
    return sha.lower()


def _extract_modelscope_sha(data: dict, file_path: str) -> str:
    if data.get("Success") is False or (
        isinstance(data.get("Code"), int) and data.get("Code") != 200
    ):
        cause = "not_found" if data.get("Code") == 404 else "upstream_http_error"
        raise PreparationError(_RESOLVE_FAILED, details=failure_details("resolve", cause))
    payload = data.get("Data") if isinstance(data.get("Data"), dict) else data
    files = payload.get("Files")
    if isinstance(files, list):
        for item in files:
            if not isinstance(item, dict) or item.get("Path") != file_path:
                continue
            rev = item.get("Revision")
            if isinstance(rev, str) and _SHA40.fullmatch(rev):
                return rev.lower()
            break
        else:
            raise PreparationError(
                _RESOLVE_FAILED,
                details=failure_details("resolve", "not_found"),
            )
    for candidate in (
        payload.get("Revision"),
        payload.get("sha"),
        payload.get("LatestCommitter", {}).get("Id")
        if isinstance(payload.get("LatestCommitter"), dict)
        else None,
    ):
        if isinstance(candidate, str) and _SHA40.fullmatch(candidate):
            return candidate.lower()
    raise PreparationError(
        _RESOLVE_FAILED,
        details=failure_details("resolve", "upstream_http_error"),
    )


def _resolve_modelscope_revision(
    client: httpx.Client,
    parsed: ResolvableModelSource,
    headers: dict[str, str],
) -> str:
    query = urlencode({"Revision": parsed.ref, "Recursive": "True"})
    url = f"https://modelscope.cn/api/v1/models/{parsed.repository}/repo/files?{query}"
    resp = client.get(url, headers=headers)
    if resp.status_code != 200:
        raise PreparationError(
            _RESOLVE_FAILED,
            details=failure_details("resolve", classify_http_status(resp.status_code)),
        )
    return _extract_modelscope_sha(_parse_json_object(resp, stage="resolve"), parsed.path)


def _probe_download_bytes(
    client: httpx.Client,
    download_url: str,
    headers: dict[str, str],
) -> int | None:
    resp = client.head(download_url, headers=headers)
    if resp.status_code in (401, 403, 404, 429):
        raise PreparationError(
            _RESOLVE_FAILED,
            details=failure_details("probe", classify_http_status(resp.status_code)),
        )
    if resp.status_code in (405, 501):
        return None
    if resp.status_code >= 400:
        raise PreparationError(
            _RESOLVE_FAILED,
            details=failure_details("probe", classify_http_status(resp.status_code)),
        )
    return parse_content_length(resp.headers)


def _resolve_with_client(
    client: httpx.Client,
    parsed: ResolvableModelSource,
    token: str | None,
) -> dict:
    already_pinned = bool(_SHA40.fullmatch(parsed.ref))
    api_headers = {"Accept": "application/json"}
    head_headers = {"Accept-Encoding": "identity"}
    if token is not None:
        api_headers["Authorization"] = f"Bearer {token}"
        head_headers["Authorization"] = f"Bearer {token}"

    if already_pinned:
        revision = parsed.ref.lower()
    elif parsed.platform == "huggingface":
        revision = _resolve_huggingface_revision(client, parsed, api_headers)
    else:
        revision = _resolve_modelscope_revision(client, parsed, api_headers)

    pinned_url = (
        f"https://{parsed.host}/{parsed.repository}/blob/{revision}/"
        f"{quote(parsed.path, safe='/')}"
    )
    source = parse_source(pinned_url)
    download_url = source.download_url
    content_length = _probe_download_bytes(client, download_url, head_headers)
    return {
        "definition": "model-resolve.v1",
        "platform": source.platform,
        "repository": source.repository,
        "revision": source.revision,
        "path": source.path,
        "pinned_url": pinned_url,
        "download_url": download_url,
        "filename": source.path.rsplit("/", 1)[-1],
        "bytes": content_length,
        "already_pinned": already_pinned,
        "model_requests_sent": 0,
    }


def resolve_model_url(
    url: str,
    *,
    token_env: str | None = None,
    timeout_seconds: int | float = 30,
    proxy_origin: str | None = None,
    client: httpx.Client | None = None,
) -> dict:
    """Resolve one HF/ModelScope GGUF URL to a pinned commit revision and optional size."""
    parsed = parse_resolvable_source(url)
    validate_proxy_origin(proxy_origin)
    _timeout(timeout_seconds)
    token = _token(token_env)
    auth_origin = origin_of(f"https://{parsed.host}") if token is not None else None
    try:
        if client is not None:
            return _resolve_with_client(client, parsed, token)
        with open_http_client(
            timeout_seconds,
            proxy_origin=proxy_origin,
            auth_origin=auth_origin,
            timeout_factory=_timeout,
        ) as owned:
            return _resolve_with_client(owned, parsed, token)
    except PreparationError:
        raise
    except httpx.HTTPError as exc:
        raise PreparationError(
            _RESOLVE_FAILED,
            details=failure_details("resolve", classify_httpx_error(exc)),
        ) from None
