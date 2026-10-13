"""One report-local copy of each text, addressed by its UTF-8 content hash."""

import hashlib

from inferyard.evidence.storage import EvidenceError
from inferyard.reporting.svg_render import check_svg, extract_svg


def text_ref(contents, text):
    if text is None:
        return None
    if type(text) is not str:
        raise EvidenceError("invalid_report_content")
    key = hashlib.sha256(text.encode("utf-8")).hexdigest()
    contents.setdefault(key, text)
    return key


def svg_reference(content, reference):
    candidate = extract_svg(content)
    checked = check_svg(candidate)
    start = content.find(candidate) if checked["status"] == "ok" else None
    return {
        "status": checked["status"],
        "bytes": checked["bytes"],
        "content_ref": reference,
        "start": start,
        "end": start + len(candidate) if start is not None else None,
    }


def validate_contents(index):
    contents = index.get("contents")
    if type(contents) is not dict:
        raise EvidenceError("invalid_report_content")
    for key, text in contents.items():
        if type(text) is not str or hashlib.sha256(text.encode("utf-8")).hexdigest() != key:
            raise EvidenceError("report_content_hash_mismatch")
    for run in index["runs"]:
        for row in run["requests"]:
            for name in ("prompt_ref", "content_ref", "reference_answer_ref", "reasoning_ref"):
                ref = row[name]
                if ref is not None and ref not in contents:
                    raise EvidenceError("report_content_reference_missing")
