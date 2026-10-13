"""Deterministic, informational SVG extraction; original markup is never rewritten."""

import re
from xml.etree import ElementTree

MAX_SVG_CHARACTERS = 512 * 1024
_FENCE = re.compile(r"^\s*```svg[^\S\r\n]*\r?\n(.*?)^\s*```[^\S\r\n]*$", re.M | re.S)
_SVG = re.compile(r"<svg(?=[\s>])[^>]*>.*?</svg\s*>", re.S)
_SMIL_LOCAL_NAMES = {"animate", "animatetransform", "animatemotion", "set"}
_KEYFRAMES = re.compile(r"@keyframes", re.I)
_ANIMATION_DECLARATION = re.compile(r"(?<![\w-])animation\s*:", re.I)


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


def _local_name(tag) -> str:
    return tag.rsplit("}", 1)[-1].lower() if isinstance(tag, str) else ""


def _svg_root(text: str) -> ElementTree.Element:
    root = ElementTree.fromstring(text, parser=ElementTree.XMLParser(target=_NoDTD()))
    if root.tag not in ("svg", "{http://www.w3.org/2000/svg}svg"):
        raise ValueError("SVG root required")
    return root


def check_svg(text: str | None) -> dict:
    """Check XML and the character limit, returning UTF-8 bytes for display only."""
    size = len(text.encode("utf-8")) if text is not None else 0
    result = {"status": "not_found", "svg": None, "bytes": size}
    if text is None:
        return result
    if len(text) > MAX_SVG_CHARACTERS:
        return {**result, "status": "too_large"}
    try:
        _svg_root(text)
    except ElementTree.ParseError, ValueError:
        return {**result, "status": "malformed"}
    return {**result, "status": "ok", "svg": text}


def animation_stats(content: str | None) -> dict:
    """Informational animation-element counts for report display; never a verdict.

    Recomputed at render time from the stored model output; the result is display
    only and is never written into index.json or sealed evidence.
    """
    result = {
        "status": "not_found",
        "reason": "svg_not_extracted",
        "smil_elements": None,
        "keyframes_rules": None,
        "animation_declarations": None,
    }
    candidate = extract_svg(content) if content is not None else None
    if candidate is None:
        return result
    if len(candidate) > MAX_SVG_CHARACTERS:
        return {**result, "status": "too_large", "reason": "svg_too_large"}
    try:
        root = _svg_root(candidate)
    except ElementTree.ParseError, ValueError:
        return {**result, "status": "malformed", "reason": "svg_malformed"}
    styles = [
        element.text
        for element in root.iter()
        if _local_name(element.tag) == "style" and element.text
    ]
    return {
        "status": "ok",
        "reason": None,
        "smil_elements": sum(
            1 for element in root.iter() if _local_name(element.tag) in _SMIL_LOCAL_NAMES
        ),
        "keyframes_rules": sum(len(_KEYFRAMES.findall(text)) for text in styles),
        "animation_declarations": sum(len(_ANIMATION_DECLARATION.findall(text)) for text in styles),
    }
