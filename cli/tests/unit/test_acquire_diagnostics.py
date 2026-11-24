"""Sanitized failure diagnostics for model acquisition without secret echo."""

import json

from inferyard.cli import main

REVISION = "499bc8821c6b12b4e53c5bffcb21ec206f212d81"
SOURCE = (
    f"https://huggingface.co/ggml-org/models-moved/blob/{REVISION}/tinyllamas/stories260K.gguf"
)
SECRET = "SUPER_SECRET_TOKEN_VALUE_12345"


def test_cli_model_acquire_sanitized_failure_without_secret_echo(monkeypatch, tmp_path, capsys):
    monkeypatch.setenv("MY_HF_SECRET", f"{SECRET}\n")
    out = tmp_path / "new"
    code = main(
        [
            "model",
            "acquire",
            "--source",
            SOURCE,
            "--out",
            str(out),
            "--token-env",
            "MY_HF_SECRET",
        ]
    )
    captured = capsys.readouterr()
    assert code == 2
    payload = json.loads(captured.out)
    assert payload["limitations"] == ["model_acquire_incomplete"]
    assert payload["details"] == {
        "failure_stage": "token",
        "failure_cause": "token_env_missing",
    }
    assert "model acquisition failed: model_acquire_incomplete (token_env_missing)" in captured.err
    assert SECRET not in captured.out + captured.err
    assert "MY_HF_SECRET" not in captured.out + captured.err
