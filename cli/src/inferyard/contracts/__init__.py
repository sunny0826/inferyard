"""Public API for the sole active data contract."""

from inferyard.errors import ContractError

from .validation import (
    Document,
    ExecutionState,
    QualityState,
    strict_json_loads,
    validate_document,
)

__all__ = [
    "ContractError",
    "Document",
    "ExecutionState",
    "QualityState",
    "strict_json_loads",
    "validate_document",
]
