from copy import deepcopy

import pytest

from inferyard.contracts.model_lineage import validate_lineage
from inferyard.config.planning import compile_plan
from inferyard.errors import ContractError
from tests.unit.phase2_contract_helpers import experiment


def lineage():
    return {
        "definition": "declared_quantization_lineage.v1",
        "base_repo": "example/base",
        "base_revision": "a" * 40,
        "base_artifacts": [{"name": "weights", "sha256": "b" * 64}],
        "tokenizer_sha256": "c" * 64,
        "conversion_tool_sha256": "d" * 64,
        "conversion_recipe_sha256": "e" * 64,
        "quantization_tool_sha256": "f" * 64,
        "quantization_recipe_sha256": "1" * 64,
        "output_sha256": "2" * 64,
        "packing": "Q4_K_M",
    }
