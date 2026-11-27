"""Recompute template identity from frozen bytes without reading external paths."""

import hashlib
import re

PRISM_BUILD = "b10743-adfffbe41"
PRISM_TEMPLATE_PROFILE = "prism.adfffbe41.lexer_source.v1"
_ACTUAL_BUILD_RE = re.compile(r"^b(\d+)-([0-9a-f]{7,40})$")
_DECLARED_RELEASE_RE = re.compile(r"^(?:prism-)?b(\d+)-([0-9a-f]{7,40})$")


def matches_prism_build(declared_release: str, actual_build_info: object) -> bool:
    if not isinstance(actual_build_info, str):
        return False
    actual = _ACTUAL_BUILD_RE.fullmatch(actual_build_info)
    if actual is None or not isinstance(declared_release, str) or not declared_release.strip():
        return False
    declared = _DECLARED_RELEASE_RE.fullmatch(declared_release)
    if declared is None:
        return actual_build_info == PRISM_BUILD
    if declared.group(1) != actual.group(1):
        return False
    declared_hex, actual_hex = declared.group(2), actual.group(2)
    return actual_hex.startswith(declared_hex) or declared_hex.startswith(actual_hex)


def prism_source(source):
    normalized = source.replace("\r\n", "\n").replace("\r", "\n")
    return normalized[:-1] if source.endswith("\n") else normalized


def _matches_template(model, properties, binding):
    service = properties.get("chat_template")
    if not isinstance(service, str):
        return False
    if binding is None:
        return hashlib.sha256(service.encode()).hexdigest() == model["template_sha256"]
    build_info = properties.get("build_info")
    if (
        not isinstance(binding, dict)
        or set(binding) != {"profile", "source"}
        or binding["profile"] != PRISM_TEMPLATE_PROFILE
        or not isinstance(build_info, str)
        or not build_info
        or not isinstance(binding["source"], str)
    ):
        return False
    source = binding["source"]
    if hashlib.sha256(source.encode()).hexdigest() != model["template_sha256"]:
        return False
    if build_info == PRISM_BUILD:
        return prism_source(source) == service
    return prism_source(source) == service or source == service


def matches_template(model, properties, binding=None):
    try:
        return _matches_template(model, properties, binding)
    except UnicodeEncodeError:
        return False
