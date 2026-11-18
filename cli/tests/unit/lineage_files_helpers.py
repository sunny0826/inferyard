import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

from inferyard.errors import ContractError
from inferyard.evidence.lineage_files import verify_file, verify_lineage_files
from tests.integration.test_lineage_records import encoded, records
from tests.unit.model_lineage_helpers import lineage


def fixture(tmp_path):
    declared = lineage()
    bindings = {}
    for key in (
        "tokenizer",
        "conversion_tool",
        "conversion_recipe",
        "quantization_tool",
        "quantization_recipe",
        "output",
        "base:weights",
        "intermediate",
    ):
        content = (key * 100).encode()
        path = tmp_path / key.replace(":", "-")
        path.write_bytes(content)
        bindings[key] = path.name
        digest = hashlib.sha256(content).hexdigest()
        if key == "base:weights":
            declared["base_artifacts"][0]["sha256"] = digest
        elif key != "intermediate":
            declared[key + "_sha256"] = digest
    receipts = records(declared)
    digest = hashlib.sha256((tmp_path / "intermediate").read_bytes()).hexdigest()
    for name, key in (("conversion", "output_sha256"), ("quantization", "input_sha256")):
        record = json.loads(receipts[name]["text"])
        record[key] = digest
        receipts[name] = encoded(record)
    return {"model_lineage": declared, "model_lineage_records": receipts}, bindings
