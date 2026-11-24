"""Unit tests for deterministic SVG extraction and validation."""

from inferyard.reporting.svg_extract import MAX_SVG_BYTES, extract_svg, inspect_svg_content


def test_extract_svg_strips_nested_and_unclosed_think_blocks_without_harming_cdata():
    content = (
        "<think>Draft: <svg><rect/></svg></think>\n"
        "Here is the pelican:\n"
        "```svg\n"
        '<svg xmlns="http://www.w3.org/2000/svg"><circle r="5">'
        '<animate attributeName="r" values="5;8;5" dur="1s" repeatCount="indefinite"/>'
        "</circle></svg>\n"
        "```\n"
    )
    extracted = extract_svg(content)
    assert extracted is not None
    assert "<rect/>" not in extracted
    assert '<animate attributeName="r"' in extracted

    nested_unclosed = "<think>outer <think>inner</think> still thinking <svg><rect/></svg>"
    assert extract_svg(nested_unclosed) is None

    nested_closed = "<think>a <think>b</think> c <svg><rect/></svg></think><svg><circle/></svg>"
    assert extract_svg(nested_closed) == "<svg><circle/></svg>"

    cdata_svg = "<svg><![CDATA[<think>]]></svg>"
    assert inspect_svg_content(cdata_svg) == {
        "status": "ok",
        "svg": cdata_svg,
        "bytes": len(cdata_svg.encode("utf-8")),
    }


def test_inspect_svg_preserves_nested_and_animated_markup_verbatim():
    raw_svg = (
        '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">'
        "<style>@keyframes spin{to{transform:rotate(360deg)}} .w{animation:spin 2s linear infinite}</style>"
        '<svg x="10" y="10"><g class="w"><circle cx="50" cy="50" r="20"/></g></svg>'
        "</svg>"
    )
    result = inspect_svg_content(f"Answer (a < b and <svg> mention):\n{raw_svg}\nDone.")
    assert result == {
        "status": "ok",
        "svg": raw_svg,
        "bytes": len(raw_svg.encode("utf-8")),
    }


def test_extract_svg_fence_consistent_across_lf_and_crlf():
    lf = "Before <svg><rect/></svg>\n```svg\n<svg><circle/></svg>\n```\n"
    crlf = lf.replace("\n", "\r\n")
    assert extract_svg(lf) == "<svg><circle/></svg>"
    assert extract_svg(crlf) == "<svg><circle/></svg>"


def test_prefixed_bare_svg_handles_comments_wrapper_and_inline_tradeoff():
    commented = "Here is the result:\n<!-- <svg><rect/></svg> --><svg><circle/></svg>"
    assert inspect_svg_content(commented) == {
        "status": "ok",
        "svg": "<svg><circle/></svg>",
        "bytes": len(b"<svg><circle/></svg>"),
    }

    wrapped = "Here is the result:\n<html><body><svg><path/></svg></body></html>"
    assert inspect_svg_content(wrapped)["status"] == "malformed"

    prefixed_utf16 = 'Here is the result:\n<?xml version="1.0" encoding="UTF-16"?><svg/>'
    assert inspect_svg_content(prefixed_utf16)["status"] == "malformed"

    same_line = "Here is the result: <svg><circle/></svg>"
    assert inspect_svg_content(same_line) == {"status": "not_found", "svg": None, "bytes": 0}

    first_line_html = "Here is the result: <html>\n<svg><circle/></svg>\n</html>"
    assert inspect_svg_content(first_line_html)["status"] == "malformed"

    first_line_utf16 = 'Here is the result: <?xml version="1.0" encoding="UTF-16"?>\n<svg><circle/></svg>'
    assert inspect_svg_content(first_line_utf16)["status"] == "malformed"

    first_line_outer_svg = (
        'Here is the result: <svg viewBox="0 0 100 100">\n'
        "<svg><circle/></svg>\n"
        "</svg>"
    )
    expected_outer = '<svg viewBox="0 0 100 100">\n<svg><circle/></svg>\n</svg>'
    assert inspect_svg_content(first_line_outer_svg) == {
        "status": "ok",
        "svg": expected_outer,
        "bytes": len(expected_outer.encode("utf-8")),
    }

    multiline_comment = "Here is the result: <!-- draft\n<svg><rect/></svg>\n--><svg><circle/></svg>"
    assert inspect_svg_content(multiline_comment) == {
        "status": "ok",
        "svg": "<svg><circle/></svg>",
        "bytes": len(b"<svg><circle/></svg>"),
    }

    closed_prose_tag = "Use <g></g> for grouping:\n<svg><g><circle/></g></svg>"
    assert inspect_svg_content(closed_prose_tag) == {
        "status": "ok",
        "svg": "<svg><g><circle/></g></svg>",
        "bytes": len(b"<svg><g><circle/></g></svg>"),
    }

    truncated_outer_svg = 'Here is the result: <svg viewBox="0 0 100 100">\n<svg><circle/></svg>'
    assert inspect_svg_content(truncated_outer_svg)["status"] == "malformed"


def test_inspect_svg_rejects_dtd_non_svg_root_encoding_errors_and_oversized():
    assert inspect_svg_content("<!DOCTYPE svg><svg><circle/></svg>")["status"] == "malformed"
    assert (
        inspect_svg_content("<html><body><svg><path/></svg></body></html>")["status"]
        == "malformed"
    )
    dtd = '<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "x"><svg/>'
    assert inspect_svg_content(f"```svg\n{dtd}\n```")["status"] == "malformed"

    entity = '<!ENTITY xxe "boom"><svg/>'
    assert inspect_svg_content(f"```svg\n{entity}\n```")["status"] == "malformed"

    wrong_encoding = '<?xml version="1.0" encoding="UTF-16"?><svg/>'
    assert inspect_svg_content(wrong_encoding)["status"] == "malformed"
    assert inspect_svg_content(f"```svg\n{wrong_encoding}\n```")["status"] == "malformed"

    unknown_encoding = '<?xml version="1.0" encoding="UTF8X"?><svg/>'
    assert inspect_svg_content(unknown_encoding)["status"] == "malformed"
    assert inspect_svg_content(f"```svg\n{unknown_encoding}\n```")["status"] == "malformed"

    assert inspect_svg_content("<svg><g></svg>")["status"] == "malformed"
    assert inspect_svg_content("```svg\n<div>not svg</div>\n```")["status"] == "malformed"
    assert inspect_svg_content("plain text") == {"status": "not_found", "svg": None, "bytes": 0}
    assert inspect_svg_content(None) == {"status": "not_found", "svg": None, "bytes": 0}

    oversized = "<svg>" + ("a" * MAX_SVG_BYTES) + "</svg>"
    result = inspect_svg_content(oversized)
    assert result["status"] == "too_large"
    assert result["svg"] is None
    assert result["bytes"] == len(oversized.encode("utf-8"))
