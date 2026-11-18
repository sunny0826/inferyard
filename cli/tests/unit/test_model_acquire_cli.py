"""Preparation-only model acquisition; fixtures never send network requests."""

import json
import subprocess
import sys

import pytest

from inferyard.cli import main

REVISION = "499bc8821c6b12b4e53c5bffcb21ec206f212d81"
SOURCE = f"https://huggingface.co/ggml-org/models-moved/blob/{REVISION}/tinyllamas/stories260K.gguf"


@pytest.mark.parametrize(
    "source",
    [
        SOURCE.replace("https:", "http:"),
        SOURCE.replace("huggingface.co", "evil.test"),
        SOURCE.replace(REVISION, "main"),
        SOURCE.replace(REVISION, "master"),
        SOURCE.replace(REVISION, "a" * 39),
        SOURCE.replace(".gguf", ".bin"),
        SOURCE + "/",
        SOURCE + "?token=SECRET",
        SOURCE + "#SECRET",
        SOURCE.replace("huggingface.co", "SECRET@huggingface.co"),
        SOURCE.replace("tinyllamas", "模型"),
        SOURCE.replace("tinyllamas", "%E6%A8%A1%E5%9E%8B"),
        SOURCE.replace("tinyllamas", ".."),
        SOURCE.replace("tinyllamas", "%2e%2e"),
        SOURCE.replace("tinyllamas", "%2f"),
        SOURCE.replace("huggingface.co", "huggingface.co:443"),
        SOURCE.replace("/blob/", "/resolve/"),
    ],
)
def test_invalid_source_uses_fixed_reason_without_echo(source, tmp_path, capsys):
    out = tmp_path / "new"
    assert main(["model", "acquire", "--source", source, "--out", str(out)]) == 2
    captured = capsys.readouterr()
    assert json.loads(captured.out)["limitations"] == ["invalid_model_source"]
    assert "SECRET" not in captured.out + captured.err
    assert not out.exists()
