import json
import shutil
import socket
import ssl
import subprocess
import threading

import pytest

from inferyard.node.join import PairingError, join
from inferyard.node.tls import der_from_pem, spki_sha256


def certificates(tmp_path):
    if shutil.which("openssl") is None:
        pytest.skip("openssl is required for mTLS loopback tests")
    ca_key, ca = tmp_path / "ca.key", tmp_path / "ca.pem"
    key, csr, cert = tmp_path / "node.key", tmp_path / "node.csr", tmp_path / "node.pem"
    subprocess.run(
        ["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", str(ca_key),
         "-out", str(ca), "-subj", "/CN=InferYard Test CA", "-days", "1"],
        check=True, capture_output=True,
    )
    subprocess.run(
        ["openssl", "req", "-newkey", "rsa:2048", "-nodes", "-keyout", str(key),
         "-out", str(csr), "-subj", "/CN=127.0.0.1"], check=True, capture_output=True,
    )
    subprocess.run(
        ["openssl", "x509", "-req", "-in", str(csr), "-CA", str(ca), "-CAkey", str(ca_key),
         "-CAcreateserial", "-out", str(cert), "-days", "1"], check=True, capture_output=True,
    )
    fullchain = tmp_path / "fullchain.pem"
    fullchain.write_text(cert.read_text() + ca.read_text())
    return {
        "ca": ca, "key": key, "cert": cert, "fullchain": fullchain,
        "fingerprint": spki_sha256(der_from_pem(ca.read_text())),
    }


def server_context(paths):
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.load_cert_chain(paths["fullchain"], paths["key"])
    return context


def response_for(paths, decision):
    if decision == "reject_pair":
        return {"ok": False, "decision": decision, "message": "rejected by user"}
    return {
        "ok": True,
        "decision": decision,
        "node_id": "node-test",
        "ca_cert_pem": paths["ca"].read_text(),
        "client_cert_pem": paths["cert"].read_text(),
        "client_key_pem": paths["key"].read_text(),
        "hub_addrs": [],
    }


def test_pairing_approval_persists_restricted_credentials(tmp_path, monkeypatch):
    paths = certificates(tmp_path)
    listener = socket.socket()
    listener.bind(("127.0.0.1", 0))
    listener.listen(2)
    port = listener.getsockname()[1]
    observed = []

    def serve():
        context = server_context(paths)
        for index in range(2):
            raw, _ = listener.accept()
            with context.wrap_socket(raw, server_side=True) as tls:
                stream = tls.makefile("rb")
                line = stream.readline()
                observed.append(line)
                if index == 0:
                    assert line == b"INFERYARD_PAIR 1\n"
                    hello = json.loads(stream.readline())
                    assert hello["code"] == "839204"
                    response = response_for(paths, "approve_pair")
                    response["hub_addrs"] = [f"127.0.0.1:{port}"]
                    tls.sendall((json.dumps(response) + "\n").encode())
                else:
                    assert line == b"INFERYARD_CONTROL 1\n"
        listener.close()

    thread = threading.Thread(target=serve, daemon=True)
    thread.start()
    monkeypatch.setattr("inferyard.node.join.device_snapshot", lambda: {"cpu_model": "test"})
    state = tmp_path / "state"
    result = join(
        hubs=f"127.0.0.1:{port}", code="839-204", ca_sha256=paths["fingerprint"],
        state_dir=state, no_autostart=True,
    )
    thread.join(timeout=5)
    assert result["node_id"] == "node-test"
    assert observed == [b"INFERYARD_PAIR 1\n", b"INFERYARD_CONTROL 1\n"]
    assert (state / "agent_config.json").is_file()
    assert "client_key_pem" not in (state / "agent_config.json").read_text()
    assert (state / "client.key").stat().st_mode & 0o777 == 0o600
    assert state.stat().st_mode & 0o777 == 0o700


def test_pairing_rejection_never_writes_credentials(tmp_path, monkeypatch):
    paths = certificates(tmp_path)
    listener = socket.socket()
    listener.bind(("127.0.0.1", 0))
    listener.listen(1)
    port = listener.getsockname()[1]

    def serve():
        raw, _ = listener.accept()
        with server_context(paths).wrap_socket(raw, server_side=True) as tls:
            stream = tls.makefile("rb")
            assert stream.readline() == b"INFERYARD_PAIR 1\n"
            stream.readline()
            response = response_for(paths, "reject_pair")
            tls.sendall((json.dumps(response) + "\n").encode())
        listener.close()

    thread = threading.Thread(target=serve, daemon=True)
    thread.start()
    monkeypatch.setattr("inferyard.node.join.device_snapshot", lambda: {})
    state = tmp_path / "rejected"
    with pytest.raises(PairingError, match="rejected") as caught:
        join(
            hubs=f"127.0.0.1:{port}", code="839204", ca_sha256=paths["fingerprint"],
            state_dir=state, no_autostart=True,
        )
    thread.join(timeout=5)
    assert caught.value.reason == "pair_rejected"
    assert not state.exists()
