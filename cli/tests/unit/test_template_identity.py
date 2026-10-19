import hashlib

import pytest

from inferyard.evidence.template_identity import (
    PRISM_BUILD,
    PRISM_TEMPLATE_PROFILE,
    matches_template,
)


def binding(source):
    return {"profile": PRISM_TEMPLATE_PROFILE, "source": source}


@pytest.mark.parametrize(
    "source,service",
    [("a\n", "a"), ("a\r\nb\r\n", "a\nb"), ("a\rb\r", "a\nb\n"), (" a \n\n", " a \n")],
)
def test_offline_recomputes_original_and_service_identity(source, service):
    model = {"template_sha256": hashlib.sha256(source.encode()).hexdigest()}
    props = {"build_info": PRISM_BUILD, "chat_template": service}
    assert matches_template(model, props, binding(source))
    assert not matches_template(model, props)


@pytest.mark.parametrize(
    "receipt,build,service",
    [
        (binding("b\n"), PRISM_BUILD, "a"),
        (binding("a"), PRISM_BUILD, "a"),
        (binding("a\n"), "unknown", "a"),
        ({"profile": "unknown", "source": "a\n"}, PRISM_BUILD, "a"),
        ({"profile": PRISM_TEMPLATE_PROFILE, "source": 1}, PRISM_BUILD, "a"),
        (binding("a\n"), PRISM_BUILD, "a\n"),
        (binding("a\n"), PRISM_BUILD, None),
        (binding("\ud800"), PRISM_BUILD, "a"),
        ({}, PRISM_BUILD, "a"),
        ([], PRISM_BUILD, "a"),
    ],
)
def test_offline_rejects_bad_binding(receipt, build, service):
    model = {"template_sha256": hashlib.sha256(b"a\n").hexdigest()}
    assert not matches_template(model, {"build_info": build, "chat_template": service}, receipt)


def test_existing_evidence_without_binding_requires_exact_raw_hash():
    model = {"template_sha256": hashlib.sha256(b"a\n").hexdigest()}
    assert matches_template(model, {"chat_template": "a\n"})
    assert not matches_template(model, {"chat_template": "a"})
    assert not matches_template(model, {"chat_template": "\ud800"})
