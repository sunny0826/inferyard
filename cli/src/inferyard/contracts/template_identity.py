"""Recompute template identity from frozen bytes without reading external paths."""

import hashlib

PRISM_BUILD = "b10743-adfffbe41"
PRISM_TEMPLATE_PROFILE = "prism.adfffbe41.lexer_source.v1"


def prism_source(source):
    normalized = source.replace("\r\n", "\n").replace("\r", "\n")
    return normalized[:-1] if source.endswith("\n") else normalized


def _matches_template(model, properties, binding):
    service = properties.get("chat_template")
    if not isinstance(service, str):
        return False
    if binding is None:
        return hashlib.sha256(service.encode()).hexdigest() == model["template_sha256"]
    if (
        not isinstance(binding, dict)
        or set(binding) != {"profile", "source"}
        or binding["profile"] != PRISM_TEMPLATE_PROFILE
        or properties.get("build_info") != PRISM_BUILD
        or not isinstance(binding["source"], str)
    ):
        return False
    source = binding["source"]
    return (
        hashlib.sha256(source.encode()).hexdigest() == model["template_sha256"]
        and prism_source(source) == service
    )


def matches_template(model, properties, binding=None):
    try:
        return _matches_template(model, properties, binding)
    except UnicodeEncodeError:
        return False
