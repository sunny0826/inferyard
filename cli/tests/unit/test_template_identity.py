import hashlib

import pytest

from inferyard.contracts.template_identity import (
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
