"""F01 input validation; no model, process or network needed."""

from pathlib import Path

import pytest

from inferyard.config.loader import load_config
from inferyard.contracts.endpoint import validate_endpoint
from inferyard.errors import ContractError


@pytest.mark.parametrize(
    "endpoint",
    [
        "http://example.com",
        "http://192.168.1.1",
        "file:///tmp/a",
        "http://127.0.0.1:0",
        "http://user:fixture-secret-7f39@localhost",
        "http://localhost?api_key=fixture-secret-7f39",
        "http://localhost/#anything",
        "http://localhost/v1",
        "http://localhost:invalid",
        "http://localhοst",
        "http://localhost\n",
        "http://[::ffff:192.168.1.1]",
    ],
)
def test_endpoint_rejections_do_not_echo_values(endpoint):
    with pytest.raises(ContractError) as error:
        validate_endpoint(endpoint)
    assert "fixture-secret-7f39" not in str(error.value)
