"""Manual redirect policy and bounded HTTP reads; no SDK, retries or credentials cache."""

import re
import time
from contextlib import contextmanager
from urllib.parse import parse_qsl, urljoin, urlsplit

import httpx

from inferyard.config.preparation_io import PreparationError

INCOMPLETE = "model_acquire_incomplete"
METADATA_LIMIT = 16 * 1024 * 1024
SIGNED_KEYS = frozenset(
    {
        "Expires",
        "Policy",
        "Signature",
        "Key-Pair-Id",
        "OSSAccessKeyId",
        "security-token",
        "X-Xet-Cas-Uid",
        "user_id",
        "xip",
        "Hash-Algorithm",
        "response-content-disposition",
        "response-content-type",
        "X-Amz-Algorithm",
        "X-Amz-Credential",
        "X-Amz-Date",
        "X-Amz-Expires",
        "X-Amz-SignedHeaders",
        "X-Amz-Signature",
        "X-Amz-Security-Token",
        "x-id",
        "x-oss-signature-version",
        "x-oss-credential",
        "x-oss-date",
        "x-oss-expires",
        "x-oss-additional-headers",
        "x-oss-signature",
        "x-oss-security-token",
    }
)
SIGNATURE_KEYS = frozenset({"Signature", "X-Amz-Signature", "x-oss-signature"})


def redirect_url(source, current, location, *, metadata=False):
    try:
        if (
            not isinstance(location, str)
            or not location.isascii()
            or "#" in location
            or any(ord(c) < 33 or ord(c) == 127 for c in location)
            or "\\" in location
            or re.search(r"%(?![0-9a-fA-F]{2})", location)
        ):
            raise ValueError
        target = urljoin(current, location)
        parsed = urlsplit(target)
        host = parsed.netloc.lower()
        suffix = ".hf.co" if source.platform == "huggingface" else ".modelscope.cn"
        if (
            parsed.scheme != "https"
            or parsed.username is not None
            or parsed.password is not None
            or parsed.port is not None
            or not re.fullmatch(r"[a-z0-9.-]+", host)
            or host.startswith(".")
            or ".." in host
        ):
            raise ValueError
        if metadata:
            if host != source.host:
                raise ValueError
        elif host == source.host:
            if parsed.query and parsed.query != urlsplit(source.download_url).query:
                raise ValueError
        else:
            if not host.endswith(suffix):
                raise ValueError
            if parsed.query:
                pairs = parse_qsl(
                    parsed.query, keep_blank_values=True, strict_parsing=True, max_num_fields=32
                )
                keys = [k for k, _ in pairs]
                if (
                    len(keys) != len(set(keys))
                    or not set(keys) <= SIGNED_KEYS
                    or not set(keys) & SIGNATURE_KEYS
                ):
                    raise ValueError
        return target
    except ValueError:
        raise PreparationError(INCOMPLETE) from None


def remaining(deadline):
    result = deadline - time.monotonic()
    if result <= 0:
        raise PreparationError(INCOMPLETE)
    return result


@contextmanager
def response(client, source, url, token, budget, deadline, *, metadata=False):
    """Build fresh requests so neither Authorization nor cookies can cross to a CDN."""
    opened = None
    try:
        for hop in range(2):
            timeout = min(budget.read_seconds, remaining(deadline))
            headers = {"Accept-Encoding": "identity"}
            if token is not None and urlsplit(url).netloc.lower() == source.host:
                headers["Authorization"] = f"Bearer {token}"
            request = httpx.Request(
                "GET",
                url,
                headers=headers,
                extensions={
                    "timeout": dict(connect=timeout, read=timeout, write=timeout, pool=timeout)
                },
            )
            opened = client.send(request, stream=True, follow_redirects=False)
            if opened.status_code in (301, 302, 303, 307, 308):
                if hop:
                    raise PreparationError(INCOMPLETE)
                next_url = redirect_url(
                    source, url, opened.headers.get("location"), metadata=metadata
                )
                opened = None
                url = next_url
                continue
            if (
                opened.status_code != 200
                or opened.headers.get("content-encoding", "identity") != "identity"
            ):
                raise PreparationError(INCOMPLETE)
            yield opened
            return
        raise PreparationError(INCOMPLETE)
    except httpx.HTTPError:
        raise PreparationError(INCOMPLETE) from None
    finally:
        if opened is not None:
            opened.close()


def metadata_bytes(client, source, token, budget, deadline):
    raw = bytearray()
    with response(
        client, source, source.metadata_url, token, budget, deadline, metadata=True
    ) as opened:
        for chunk in opened.iter_raw(chunk_size=64 * 1024):
            remaining(deadline)
            if len(raw) + len(chunk) > METADATA_LIMIT:
                raise PreparationError(INCOMPLETE)
            raw.extend(chunk)
    return bytes(raw)
