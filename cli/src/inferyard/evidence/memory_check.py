"""Strict reads of saved memory diagnostics; validation stays in the contract module."""

from pathlib import Path

from inferyard.contracts.memory_check import validate_memory_check
from inferyard.evidence.storage import read_json


def read_memory_check(path: Path):
    data = read_json(path)
    validate_memory_check(data)
    return data
