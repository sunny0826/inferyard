"""Frozen source URL parsing, without network."""

import hashlib

import pytest

from inferyard.platforms.model_source import parse_source

REVISIONS = {
    "huggingface": "499bc8821c6b12b4e53c5bffcb21ec206f212d81",
    "modelscope": "f68dcd8c87746ccbf4f1f02703b45e393f921ce4",
}


def source(platform):
    host = "huggingface.co" if platform == "huggingface" else "modelscope.cn"
    repo = "models-moved" if platform == "huggingface" else "models"
    return parse_source(
        f"https://{host}/ggml-org/{repo}/blob/{REVISIONS[platform]}/tinyllamas/stories260K.gguf"
    )


def test_nested_ascii_filename_and_uppercase_revision():
    parsed = parse_source(
        "https://huggingface.co/owner/repo/blob/" + "A" * 40 + "/sub/file%20a.gguf"
    )
    assert parsed.path == "sub/file a.gguf"
    assert parsed.revision == "a" * 40
    assert parsed.download_url.endswith("/sub/file%20a.gguf")


def test_modelscope_download_url_carries_revision_and_path():
    parsed = source("modelscope")
    assert "Revision=" + REVISIONS["modelscope"] in parsed.download_url
    assert "FilePath=tinyllamas" in parsed.download_url


def record_for(model_path, version=3, platform="huggingface"):
    """Build a synthetic source record bound to an actual local model file."""
    parsed = source(platform)
    record = {
        "definition": f"remote_model_source.v{version}",
        "parser": "remote-model-source-parser.v1",
        "platform": parsed.platform,
        "repository": parsed.repository,
        "revision": parsed.revision,
        "path": parsed.path,
        "bytes": model_path.stat().st_size,
        "sha256": hashlib.sha256(model_path.read_bytes()).hexdigest(),
        "retrieved_at": "2026-10-11T00:00:00Z",
    }
    if version < 3:
        record["metadata_sha256"] = hashlib.sha256(b"recorded metadata").hexdigest()
    if version == 2:
        record["acquisition_budget"] = {
            "total_seconds": 14400,
            "read_seconds": 360,
            "cleanup_seconds": 120,
            "max_bytes": 100,
        }
    return record


@pytest.mark.parametrize("path", ["weights/q4+v1(foo).gguf", "weights/q4%2Bv1%28foo%29.gguf"])
def test_ascii_filename_punctuation_is_supported(path):
    parsed = parse_source("https://huggingface.co/owner/repo/blob/" + "a" * 40 + "/" + path)
    assert parsed.path == "weights/q4+v1(foo).gguf"
