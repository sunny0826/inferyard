"""Recorded immutable metadata and adversarial local inputs, without network."""

import hashlib
import json
from pathlib import Path

import pytest

from inferyard.config.preparation_io import PreparationError
from inferyard.platforms.model_source import parse_metadata, parse_source

FIXTURES = Path(__file__).parents[1] / "fixtures/model_source"
REVISIONS = {
    "huggingface": "499bc8821c6b12b4e53c5bffcb21ec206f212d81",
    "modelscope": "f68dcd8c87746ccbf4f1f02703b45e393f921ce4",
}
DIGEST = "270cba1bd5109f42d03350f60406024560464db173c0e387d91f0426d3bd256d"


def source(platform):
    host = "huggingface.co" if platform == "huggingface" else "modelscope.cn"
    repo = "models-moved" if platform == "huggingface" else "models"
    return parse_source(
        f"https://{host}/ggml-org/{repo}/blob/{REVISIONS[platform]}/tinyllamas/stories260K.gguf"
    )


@pytest.mark.parametrize("platform", REVISIONS)
def test_recorded_fixed_metadata(platform):
    raw = (FIXTURES / f"{platform}.json").read_bytes()
    record = parse_metadata(source(platform), raw)
    assert record["bytes"] == 1185376
    assert record["sha256"] == DIGEST
    assert record["metadata_sha256"] == hashlib.sha256(raw).hexdigest()
    assert record["revision"] == REVISIONS[platform]
    assert record["definition"] == "remote_model_source.v1"
    assert record["parser"] == "remote-model-source-parser.v1"


@pytest.mark.parametrize("platform", REVISIONS)
@pytest.mark.parametrize("change", ["revision", "path", "size", "sha256", "duplicate", "directory"])
def test_metadata_rejects_identity_and_invalid_fields(platform, change):
    data = json.loads((FIXTURES / f"{platform}.json").read_bytes())
    if platform == "huggingface":
        files = data["siblings"]
        chosen = next(f for f in files if f["rfilename"] == source(platform).path)
        if change == "revision":
            data["sha"] = "a" * 40
        elif change == "path":
            chosen["rfilename"] = "other.gguf"
        elif change == "size":
            chosen["size"] = True
        elif change == "sha256":
            chosen["lfs"]["sha256"] = "a" * 40
        elif change == "directory":
            del chosen["lfs"]
        else:
            files.append(chosen.copy())
    else:
        files = data["Data"]["Files"]
        chosen = next(f for f in files if f["Path"] == source(platform).path)
        if change == "revision":
            chosen["Revision"] = "master"
        elif change == "path":
            chosen["Path"] = "other.gguf"
        elif change == "size":
            chosen["Size"] = True
        elif change == "sha256":
            chosen["Sha256"] = "a" * 40
        elif change == "directory":
            chosen["Type"] = "tree"
        else:
            files.append(chosen.copy())
    with pytest.raises(PreparationError, match="model_source_metadata_mismatch"):
        parse_metadata(source(platform), json.dumps(data).encode())


@pytest.mark.parametrize("raw", [b'{"sha":1,"sha":2}', b'{"size":NaN}', b"\xff", b"[]", b"null"])
def test_bad_json_is_sanitized(raw):
    with pytest.raises(PreparationError, match="model_source_metadata_mismatch"):
        parse_metadata(source("huggingface"), raw)


def test_nested_ascii_filename_and_uppercase_revision():
    parsed = parse_source(
        "https://huggingface.co/owner/repo/blob/" + "A" * 40 + "/sub/file%20a.gguf"
    )
    assert parsed.path == "sub/file a.gguf"
    assert parsed.revision == "a" * 40
    assert parsed.download_url.endswith("/sub/file%20a.gguf")


def test_modelscope_root_file_does_not_send_dot_directory():
    from urllib.parse import parse_qs, urlsplit

    parsed = parse_source("https://modelscope.cn/owner/repo/blob/" + "a" * 40 + "/file.gguf")
    assert "Root" not in parse_qs(urlsplit(parsed.metadata_url).query)


@pytest.mark.parametrize("path", ["weights/q4+v1(foo).gguf", "weights/q4%2Bv1%28foo%29.gguf"])
def test_ascii_filename_punctuation_is_supported(path):
    parsed = parse_source("https://huggingface.co/owner/repo/blob/" + "a" * 40 + "/" + path)
    assert parsed.path == "weights/q4+v1(foo).gguf"
