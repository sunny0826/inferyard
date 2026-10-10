"""Content references are report-local, hash-bound and shared across views."""

import hashlib

import pytest

from inferyard.evidence.storage import EvidenceError, json_bytes
from inferyard.reporting.report import build_index
from inferyard.reporting.report_common import _environment, render_report_html
from inferyard.reporting.report_content import svg_reference, text_ref, validate_contents
from tests.helpers import fixture_run


def test_text_is_stored_once_and_svg_keeps_only_checked_offsets():
    contents = {}
    text = "🧪 before\n```svg\n<svg><text>共享全文</text></svg>\n```\n"
    ref = text_ref(contents, text)
    assert ref == text_ref(contents, text)
    assert ref == hashlib.sha256(text.encode()).hexdigest()
    assert list(contents.values()) == [text]
    view = svg_reference(text, ref)
    assert view["status"] == "ok" and "svg" not in view
    assert text[view["start"] : view["end"]] == "<svg><text>共享全文</text></svg>\n"


def test_report_has_internal_refs_and_one_serialized_copy(tmp_path):
    root = fixture_run(tmp_path / "source")
    index = build_index([root], tmp_path / "out")
    validate_contents(index)
    row = index["runs"][0]["requests"][0]
    assert all(field not in row for field in ("prompt", "content", "reference_answer"))
    unique = 'single-copy-marker-中文🧪\r\n<script>alert("x")</script>'
    ref = text_ref(index["contents"], unique)
    for run in index["runs"]:
        for request in run["requests"]:
            request.update(prompt_ref=ref, content_ref=ref, reference_answer_ref=ref)
    assert json_bytes(index).count(b"single-copy-marker") == 1
    html = render_report_html(_environment(), "report.html", index)
    assert html.count("single-copy-marker") == 1
    assert "<script>alert" not in html
    assert "&#13;" in html
    assert f'href="#text-{ref}"' in html  # Also readable with scripting disabled.
    index["contents"][ref] = "changed"
    with pytest.raises(EvidenceError, match="report_content_hash_mismatch"):
        render_report_html(_environment(), "report.html", index)
