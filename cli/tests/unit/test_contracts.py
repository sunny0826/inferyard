"""Published JSON schemas are valid Draft 2020-12 documents."""

from jsonschema import Draft202012Validator

from inferyard.contracts.schemas import export_schema, schemas_for


def test_all_published_schemas_are_valid():
    for kind in schemas_for():
        Draft202012Validator.check_schema(export_schema(kind))
