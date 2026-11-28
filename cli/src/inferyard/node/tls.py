"""TLS 1.3 contexts and minimal X.509 SPKI fingerprinting using only the stdlib."""

from __future__ import annotations

import base64
import hashlib
import hmac
import re
import ssl
from pathlib import Path

_HEX_64 = re.compile(r"^[0-9a-fA-F]{64}$")


def _length(data: bytes, offset: int) -> tuple[int, int]:
    if offset >= len(data):
        raise ValueError("truncated DER length")
    first = data[offset]
    if first < 0x80:
        return first, offset + 1
    count = first & 0x7F
    if count == 0 or count > 4 or offset + 1 + count > len(data):
        raise ValueError("invalid DER length")
    if data[offset + 1] == 0:
        raise ValueError("non-minimal DER length")
    return int.from_bytes(data[offset + 1 : offset + 1 + count], "big"), offset + 1 + count


def _tlv(data: bytes, offset: int) -> tuple[int, int, int, int, bytes]:
    start = offset
    if offset >= len(data):
        raise ValueError("truncated DER value")
    tag = data[offset]
    size, content = _length(data, offset + 1)
    end = content + size
    if end > len(data):
        raise ValueError("truncated DER content")
    return tag, content, end, end, data[start:end]


def _children(data: bytes, start: int, end: int) -> list[tuple[int, bytes]]:
    result = []
    while start < end:
        tag, _content, next_offset, next_offset, raw = _tlv(data, start)
        if next_offset > end:
            raise ValueError("DER child exceeds parent")
        result.append((tag, raw))
        start = next_offset
    if start != end:
        raise ValueError("invalid DER children")
    return result


def subject_public_key_info(certificate_der: bytes) -> bytes:
    """Extract the exact SubjectPublicKeyInfo TLV from a DER X.509 certificate."""
    if not isinstance(certificate_der, bytes) or not certificate_der:
        raise ValueError("empty DER certificate")
    tag, content, end, next_offset, _raw = _tlv(certificate_der, 0)
    if tag != 0x30 or next_offset != len(certificate_der):
        raise ValueError("invalid DER certificate")
    certificate = _children(certificate_der, content, end)
    if not certificate or certificate[0][0] != 0x30:
        raise ValueError("missing DER TBSCertificate")
    tbs_tag, tbs_content, tbs_end, _next, _raw = _tlv(certificate[0][1], 0)
    if tbs_tag != 0x30:
        raise ValueError("invalid DER TBSCertificate")
    fields = _children(certificate[0][1], tbs_content, tbs_end)
    index = 1 if fields and fields[0][0] == 0xA0 else 0
    expected = (0x02, 0x30, 0x30, 0x30, 0x30, 0x30)
    if len(fields) < index + 6 or tuple(tag for tag, _ in fields[index : index + 6]) != expected:
        raise ValueError("invalid DER TBSCertificate fields")
    return fields[index + 5][1]


def spki_sha256(certificate_der: bytes) -> str:
    return hashlib.sha256(subject_public_key_info(certificate_der)).hexdigest()


def der_from_pem(pem: str) -> bytes:
    match = re.search(
        r"-----BEGIN CERTIFICATE-----\s*(.*?)\s*-----END CERTIFICATE-----", pem, re.DOTALL
    )
    if match is None:
        raise ValueError("PEM certificate not found")
    try:
        return base64.b64decode(re.sub(r"\s+", "", match.group(1)), validate=True)
    except ValueError as exc:
        raise ValueError("invalid PEM certificate") from exc


def normalize_fingerprint(value: str) -> str:
    normalized = value.strip().lower().replace(":", "")
    if not _HEX_64.fullmatch(normalized):
        raise ValueError("CA fingerprint must be a SHA-256 hex value")
    return normalized


def _certificate_der(value) -> bytes:
    if isinstance(value, bytes):
        return value
    if isinstance(value, str):
        return der_from_pem(value)
    public_bytes = getattr(value, "public_bytes", None)
    if callable(public_bytes):
        encoded = public_bytes()
        return _certificate_der(encoded)
    raise ValueError("unsupported certificate representation")


def peer_certificates(tls_socket: ssl.SSLSocket) -> list[bytes]:
    values = []
    peer = tls_socket.getpeercert(binary_form=True)
    if peer:
        values.append(peer)
    for name in ("get_verified_chain", "get_unverified_chain"):
        getter = getattr(tls_socket, name, None)
        if callable(getter):
            try:
                values.extend(_certificate_der(item) for item in getter())
            except (OSError, ValueError, ssl.SSLError):
                continue
    return list(dict.fromkeys(values))


def pinned_ca_pem(certificates: list[bytes], expected: str) -> str:
    pinned = normalize_fingerprint(expected)
    for certificate in certificates:
        if hmac.compare_digest(spki_sha256(certificate), pinned):
            return ssl.DER_cert_to_PEM_cert(certificate)
    raise ssl.SSLError("pinned CA SPKI fingerprint does not match peer certificate chain")


def verify_spki_pin(certificates: list[bytes], expected: str) -> str:
    pinned_ca_pem(certificates, expected)
    return normalize_fingerprint(expected)


def pair_context(ca_pem: str | None = None) -> ssl.SSLContext:
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.maximum_version = ssl.TLSVersion.TLSv1_3
    if ca_pem is None:
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE
    else:
        context.verify_mode = ssl.CERT_REQUIRED
        context.check_hostname = True
        context.load_verify_locations(cadata=ca_pem)
    return context


def control_context(state_dir: Path, *, ca_pem: str | None = None) -> ssl.SSLContext:
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.maximum_version = ssl.TLSVersion.TLSv1_3
    context.check_hostname = False
    context.verify_mode = ssl.CERT_REQUIRED
    if ca_pem is None:
        context.load_verify_locations(cafile=str(state_dir / "ca.pem"))
    else:
        context.load_verify_locations(cadata=ca_pem)
    context.load_cert_chain(state_dir / "client.pem", state_dir / "client.key")
    return context
