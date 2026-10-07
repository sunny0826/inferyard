"""Replay recorded conversion chains without treating receipts as authentication."""

import hashlib

from inferyard.contracts.schemas_lineage import CONVERSION as CONVERSION
from inferyard.contracts.schemas_lineage import QUANTIZATION as QUANTIZATION
from inferyard.contracts.schemas_lineage import RAW_RECORD as RAW_RECORD
from inferyard.contracts.schemas_lineage import RECORDS as RECORDS
from inferyard.contracts.validation import ContractError, _validate, strict_json_loads


def parse_record(raw, schema):
    path = "experiment.workloads.model_lineage_records"
    if hashlib.sha256(raw["text"].encode("utf-8")).hexdigest() != raw["sha256"]:
        raise ContractError(path, "record bytes differ from hash")
    try:
        record = strict_json_loads(raw["text"])
    except (ValueError, RecursionError) as exc:
        raise ContractError(path, "invalid receipt JSON") from exc
    _validate(record, schema, path)
    return record


def validate_records(workload):
    records = workload.get("model_lineage_records")
    if records is None:
        return
    path = "experiment.workloads.model_lineage_records"
    lineage = workload.get("model_lineage")
    if lineage is None:
        raise ContractError(path, "receipts require lineage declaration")
    conversion = parse_record(records["conversion"], CONVERSION)
    quantization = parse_record(records["quantization"], QUANTIZATION)
    for key in ("base_repo", "base_revision", "tokenizer_sha256"):
        if conversion[key] != lineage[key]:
            raise ContractError(path, "conversion source differs from declaration")
    artifacts = conversion["base_artifacts"]
    if len({a["name"] for a in artifacts}) != len(artifacts) or sorted(
        artifacts, key=lambda a: a["name"]
    ) != sorted(lineage["base_artifacts"], key=lambda a: a["name"]):
        raise ContractError(path, "conversion artifacts differ from declaration")
    for stage, receipt in (("conversion", conversion), ("quantization", quantization)):
        for field in ("tool_sha256", "recipe_sha256"):
            if receipt[field] != lineage[stage + "_" + field]:
                raise ContractError(path, "tool or recipe differs from declaration")
    if quantization["input_sha256"] != conversion["output_sha256"]:
        raise ContractError(path, "conversion and quantization chain broken")
    for field in ("output_sha256", "packing"):
        if quantization[field] != lineage[field]:
            raise ContractError(path, "quantized output differs from declaration")
