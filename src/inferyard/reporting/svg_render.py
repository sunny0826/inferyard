"""Deterministic, informational SVG extraction; original markup is never rewritten."""

import re
from xml.etree import ElementTree

MAX_SVG_CHARACTERS = 512 * 1024
_FENCE = re.compile(r"^\s*```svg[^\S\r\n]*\r?\n(.*?)^\s*```[^\S\r\n]*$", re.M | re.S)
_SVG = re.compile(r"<svg(?=[\s>])[^>]*>.*?</svg\s*>", re.S)


def extract_svg(content: str) -> str | None:
    """Prefer the first complete svg fence, otherwise the first closed SVG substring."""
    fence = _FENCE.search(content)
    if fence:
        return fence.group(1)
    match = _SVG.search(content)
    return match.group(0) if match else None


class _NoDTD(ElementTree.TreeBuilder):
    def doctype(self, name, pubid, system):
        raise ValueError("DTD is prohibited")


def check_svg(text: str | None) -> dict:
    """Check XML and the character limit, returning UTF-8 bytes for display only."""
    size = len(text.encode("utf-8")) if text is not None else 0
    result = {"status": "not_found", "svg": None, "bytes": size}
    if text is None:
        return result
    if len(text) > MAX_SVG_CHARACTERS:
        return {**result, "status": "too_large"}
    try:
        root = ElementTree.fromstring(text, parser=ElementTree.XMLParser(target=_NoDTD()))
        if root.tag not in ("svg", "{http://www.w3.org/2000/svg}svg"):
            raise ValueError("SVG root required")
    except ElementTree.ParseError, ValueError:
        return {**result, "status": "malformed"}
    return {**result, "status": "ok", "svg": text}
