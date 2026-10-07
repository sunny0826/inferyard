from pathlib import Path

import pytest

from inferyard.contracts.validation import strict_json_loads

FIXTURES = Path(__file__).parent / "fixtures"


@pytest.fixture
def config_path():
    return FIXTURES / "config" / "valid.toml"


@pytest.fixture
def wire_fixture():
    def read(kind):
        return strict_json_loads((FIXTURES / "contracts" / f"{kind}.valid.json").read_text())

    return read
