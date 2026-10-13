"""Frozen declared lineage; hashing declarations does not authenticate their origin."""

from inferyard.contracts.schemas_lineage import LINEAGE as LINEAGE
from inferyard.contracts.schemas_lineage import REVISION as REVISION
from inferyard.contracts.validation import ContractError


def validate_lineage(workload, config):
    from inferyard.config.lineage_records import validate_records

    validate_records(workload)
    lineage = workload.get("model_lineage")
    if lineage is None:
        return
    for lineage_key, model_key in (("output_sha256", "sha256"), ("packing", "packing")):
        if lineage[lineage_key] != config["model"][model_key]:
            raise ContractError("experiment.workloads.model_lineage", "output differs from config")
    names = [a["name"] for a in lineage["base_artifacts"]]
    if len(names) != len(set(names)):
        raise ContractError("experiment.workloads.model_lineage", "duplicate base artifact name")
