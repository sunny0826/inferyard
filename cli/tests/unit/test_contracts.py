"""F01 contract slice: independently specified valid and invalid wire documents."""

from copy import deepcopy

import pytest
from jsonschema import Draft202012Validator

from inferyard.config.loader import load_config
from inferyard.contracts.schemas import export_schema, schemas_for
from inferyard.contracts.validation import Document, strict_json_loads, validate_document
from inferyard.errors import ContractError


def test_all_published_schemas_are_valid():
    for kind in schemas_for():
        Draft202012Validator.check_schema(export_schema(kind))
