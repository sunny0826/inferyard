"""Unit tests for PolyStrata setup log secret redaction."""

from __future__ import annotations

from inferyard.provisioning.polystrata_preflight import sanitize_log_line


def test_sanitize_log_line_redacts_secrets_and_ansi():
    raw = (
        "\x1b[32mOK\x1b[0m https://alice:s3cret@host/x https://opaque_token_xyz@rocm.internal/whl "
        "Bearer tok123456 hf_1234567890ab --api-key mykey"
    )
    cleaned = sanitize_log_line(raw)
    assert "\x1b" not in cleaned
    assert "s3cret" not in cleaned
    assert "opaque_token_xyz" not in cleaned
    assert "tok123456" not in cleaned
    assert "hf_1234567890ab" not in cleaned
    assert "mykey" not in cleaned
    assert "[REDACTED]" in cleaned
