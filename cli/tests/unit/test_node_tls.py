import hashlib
import shutil
import ssl
import subprocess

import pytest

from inferyard.node.tls import der_from_pem, normalize_fingerprint, spki_sha256, verify_spki_pin
from inferyard.contracts.node_protocol import normalize_pair_code


def _certificate(tmp_path):
    if shutil.which("openssl") is None:
        pytest.skip("openssl is required for the certificate fixture")
    key = tmp_path / "ca.key"
    cert = tmp_path / "ca.pem"
    subprocess.run(
        [
            "openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
            "-keyout", str(key), "-out", str(cert), "-subj", "/CN=InferYard Test CA",
            "-days", "1",
        ],
        check=True,
        capture_output=True,
    )
    der = tmp_path / "ca.der"
    subprocess.run(
        ["openssl", "x509", "-in", str(cert), "-outform", "DER", "-out", str(der)],
        check=True,
        capture_output=True,
    )
    return cert, der.read_bytes()


def test_spki_fingerprint_matches_openssl_and_pinning(tmp_path):
    cert, der = _certificate(tmp_path)
    subprocess.run(
        ["openssl", "x509", "-in", str(cert), "-pubkey", "-noout"], check=True, capture_output=True
    )
    fingerprint = spki_sha256(der)
    assert fingerprint == spki_sha256(der_from_pem(cert.read_text()))
    assert len(fingerprint) == 64
    assert verify_spki_pin([der], fingerprint.upper()) == fingerprint
    with pytest.raises(ssl.SSLError):
        verify_spki_pin([der], "0" * 64)
    assert hashlib.sha256(der).hexdigest() != fingerprint


def test_der_and_pair_code_validation(tmp_path):
    with pytest.raises(ValueError):
        spki_sha256(b"not a certificate")
    with pytest.raises(ValueError):
        der_from_pem("not pem")
    assert normalize_fingerprint("AA:" + "bb" * 31) == "aa" + "bb" * 31
    assert normalize_pair_code(" 839-204 ") == "839204"
    with pytest.raises(ValueError):
        normalize_pair_code("12345")
