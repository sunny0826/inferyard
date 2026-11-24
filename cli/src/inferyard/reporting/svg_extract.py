"""Deterministic SVG extraction and validation for Studio report display."""

import re
from xml.etree import ElementTree

MAX_SVG_BYTES = 512 * 1024
_THINK_TOKEN = re.compile(
    r"<!--|<!\[CDATA\[|<think(?:\s[^>]*)?>|</think\s*>",
    re.I,
)
_FENCE = re.compile(
    r"^[ \t]*```svg[ \t]*\r?\n(.*?)^[ \t]*```[ \t]*\r?$",
    re.M | re.S,
)
_DOC_START = re.compile(r"^[ \t]*<[?!A-Za-z]", re.M)
_SVG_OPEN = re.compile(r"<svg(?=[\s>/])")
_SVG_CLOSE = re.compile(r"</svg\s*>")
_FORBIDDEN_DECLARATION = re.compile(r"<!\s*(?:DOCTYPE|ENTITY)\b", re.I)
_LEADING_PROLOG = re.compile(r"(?:\s+|<\?.*?\?>|<!--.*?-->)+", re.S)
_XML_DECL = re.compile(r"<\?xml\b.*?\?>", re.S)
_PREFIX_DECL = re.compile(r"<\?xml\b|<!\s*(?:DOCTYPE|ENTITY)\b", re.I)
_OPEN_TAG = re.compile(r"<([A-Za-z][A-Za-z0-9:_-]*)(?=[\s>/])")
_CLOSE_TAG = re.compile(r"</([A-Za-z][A-Za-z0-9:_-]*)\s*>")


def _strip_reasoning(content: str) -> str:
    """Drop nested or unclosed <think> blocks while preserving XML comments and CDATA."""
    out: list[str] = []
    pos = 0
    depth = 0
    length = len(content)
    while pos < length:
        match = _THINK_TOKEN.search(content, pos)
        if match is None:
            if depth == 0:
                out.append(content[pos:])
            break
        start, end = match.span()
        token = match.group(0)
        if depth == 0 and start > pos:
            out.append(content[pos:start])
        if token == "<!--":
            close = content.find("-->", end)
            tail = length if close < 0 else close + 3
            if depth == 0:
                out.append(content[start:tail])
            pos = tail
        elif token == "<![CDATA[":
            close = content.find("]]>", end)
            tail = length if close < 0 else close + 3
            if depth == 0:
                out.append(content[start:tail])
            pos = tail
        elif token[:2].lower() == "</":
            if depth > 0:
                depth -= 1
            else:
                out.append(token)
            pos = end
        else:
            if not token.rstrip().endswith("/>"):
                depth += 1
            pos = end
    return "".join(out)


def _find_tag_end(text: str, pos: int) -> int:
    quote = ""
    length = len(text)
    while pos < length:
        ch = text[pos]
        if quote:
            if ch == quote:
                quote = ""
        elif ch in ("'", '"'):
            quote = ch
        elif ch == ">":
            return pos
        pos += 1
    return -1


def _extract_balanced_svg(text: str, start: int) -> str | None:
    pos = start
    depth = 0
    length = len(text)
    while pos < length:
        if text.startswith("<!--", pos):
            close = text.find("-->", pos + 4)
            if close < 0:
                return None
            pos = close + 3
            continue
        if text.startswith("<![CDATA[", pos):
            close = text.find("]]>", pos + 9)
            if close < 0:
                return None
            pos = close + 3
            continue
        if _SVG_OPEN.match(text, pos):
            end = _find_tag_end(text, pos + 4)
            if end < 0:
                return None
            if text[pos:end].rstrip().endswith("/"):
                if depth == 0:
                    return text[start : end + 1]
            else:
                depth += 1
            pos = end + 1
            continue
        close_match = _SVG_CLOSE.match(text, pos)
        if close_match:
            depth -= 1
            pos = close_match.end()
            if depth == 0:
                return text[start:pos]
            continue
        next_lt = text.find("<", pos + 1)
        if next_lt < 0:
            return None
        pos = next_lt
    return None


def _pop_open_tag(stack: list[tuple[str, int]], name: str) -> None:
    for idx in range(len(stack) - 1, -1, -1):
        if stack[idx][0] == name:
            del stack[idx:]
            return


def _rewind_doc_start(cleaned: str, start: int) -> int:
    """Keep leading declarations, unclosed comments, or still-open outer tags before start."""
    prefix = cleaned[:start]
    tail = cleaned[start:]
    earliest = start
    chain_start: int | None = None
    open_stack: list[tuple[str, int]] = []
    pos = 0
    length = len(prefix)

    while pos < length:
        if prefix[pos].isspace():
            pos += 1
            continue
        if prefix.startswith("<!--", pos):
            if chain_start is None:
                chain_start = pos
            close = prefix.find("-->", pos + 4)
            if close < 0:
                earliest = min(earliest, pos)
                break
            pos = close + 3
            continue
        if prefix.startswith("<?", pos):
            close = prefix.find("?>", pos + 2)
            if close >= 0:
                if chain_start is None:
                    chain_start = pos
                if _PREFIX_DECL.match(prefix, pos):
                    earliest = min(earliest, pos)
                pos = close + 2
                continue
        if _PREFIX_DECL.match(prefix, pos):
            end = _find_tag_end(prefix, pos + 2)
            if end >= 0:
                if chain_start is None:
                    chain_start = pos
                earliest = min(earliest, pos)
                pos = end + 1
                continue
        close_match = _CLOSE_TAG.match(prefix, pos)
        if close_match is not None:
            if chain_start is None:
                chain_start = pos
            _pop_open_tag(open_stack, close_match.group(1))
            pos = close_match.end()
            continue
        open_match = _OPEN_TAG.match(prefix, pos)
        if open_match is not None:
            end = _find_tag_end(prefix, open_match.end())
            if end >= 0:
                if chain_start is None:
                    chain_start = pos
                if not prefix[pos:end].rstrip().endswith("/"):
                    open_stack.append((open_match.group(1), pos))
                pos = end + 1
                continue
        chain_start = None
        next_lt = prefix.find("<", pos + 1)
        if next_lt < 0:
            break
        pos = next_lt

    for tag_name, tag_pos in open_stack:
        if chain_start is not None and tag_pos >= chain_start:
            earliest = min(earliest, tag_pos)
        elif tag_name != "svg" and re.search(rf"</{re.escape(tag_name)}\s*>", tail):
            earliest = min(earliest, tag_pos)

    return earliest


def extract_svg(content: str | None) -> str | None:
    """Strip inline <think> sections, then take the first ```svg fence or bare SVG."""
    if not content:
        return None
    cleaned = _strip_reasoning(content)
    fence = _FENCE.search(cleaned)
    if fence:
        candidate = fence.group(1).strip()
        return candidate or None
    doc_match = _DOC_START.search(cleaned)
    if doc_match is None:
        return None
    doc_start = _rewind_doc_start(cleaned, doc_match.start())
    doc = cleaned[doc_start:].strip()
    if _SVG_OPEN.search(doc) is None:
        return None
    prolog = _LEADING_PROLOG.match(doc)
    first_tag = prolog.end() if prolog else 0
    if _FORBIDDEN_DECLARATION.search(doc[:first_tag]) or not _SVG_OPEN.match(doc, first_tag):
        return doc
    balanced = _extract_balanced_svg(doc, first_tag)
    if balanced is None:
        return doc
    decl = _XML_DECL.search(doc[:first_tag])
    return doc[:first_tag] + balanced if decl else balanced


class _NoDTD(ElementTree.TreeBuilder):
    def doctype(self, name, pubid, system):
        raise ValueError("DTD is prohibited")


def _validate_svg_xml(raw_bytes: bytes, text: str) -> None:
    if _FORBIDDEN_DECLARATION.search(text):
        raise ValueError("DTD/ENTITY is prohibited")
    root = ElementTree.fromstring(raw_bytes, parser=ElementTree.XMLParser(target=_NoDTD()))
    if root.tag not in ("svg", "{http://www.w3.org/2000/svg}svg"):
        raise ValueError("SVG root required")


def inspect_svg_content(content: str | None) -> dict[str, str | int | None]:
    """Extract and validate an SVG document without rewriting original markup."""
    candidate = extract_svg(content)
    if candidate is None:
        return {"status": "not_found", "svg": None, "bytes": 0}
    raw_bytes = candidate.encode("utf-8")
    size = len(raw_bytes)
    if size > MAX_SVG_BYTES:
        return {"status": "too_large", "svg": None, "bytes": size}
    try:
        _validate_svg_xml(raw_bytes, candidate)
    except (ElementTree.ParseError, ValueError, LookupError):
        return {"status": "malformed", "svg": None, "bytes": size}
    return {"status": "ok", "svg": candidate, "bytes": size}
