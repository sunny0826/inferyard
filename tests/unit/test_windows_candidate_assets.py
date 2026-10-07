"""Candidate assets come from the verified model, without historical evidence files."""

import hashlib
import importlib
from pathlib import Path
from types import SimpleNamespace

import pytest


@pytest.fixture
def candidate(monkeypatch):
    monkeypatch.syspath_prepend(str(Path(__file__).resolve().parents[2] / "scripts"))
    return importlib.import_module("create_windows_candidate")


@pytest.mark.parametrize("key", ["tokenizer.chat_template", "tokenizer.chat_template.default"])
def test_model_and_template_identity_replace_example_placeholders(
    candidate, monkeypatch, tmp_path, key
):
    template = "{{ messages }}\r\n中文\n"
    model = SimpleNamespace(path=str(tmp_path / "model.gguf"), sha256="a" * 64)
    monkeypatch.setattr(candidate, "read_metadata", lambda path: {key: template})
    target = tmp_path / "candidate.chat-template.jinja"
    declaration, raw = candidate.model_declaration(model, {"model_revision": "revision"}, target)
    assert declaration == {
        "local_path": model.path,
        "sha256": model.sha256,
        "revision": "revision",
        "template_path": str(target),
        "template_sha256": hashlib.sha256(template.encode("utf-8")).hexdigest(),
    }
    assert raw == template


@pytest.mark.parametrize("template", [None, "", "  ", 42])
def test_missing_model_template_is_not_replaced_by_historical_default(
    candidate, monkeypatch, template
):
    monkeypatch.setattr(
        candidate, "read_metadata", lambda path: {"tokenizer.chat_template": template}
    )
    with pytest.raises(ValueError, match="model_chat_template_required"):
        candidate.model_declaration(SimpleNamespace(path="model.gguf"), {}, Path("template.jinja"))
