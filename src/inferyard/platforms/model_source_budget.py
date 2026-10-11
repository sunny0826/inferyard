"""Operator-declared acquisition limits, shared by execution and source records."""

import math
from dataclasses import dataclass

from inferyard.config.preparation_io import PreparationError


@dataclass(frozen=True, slots=True)
class Budget:
    total_seconds: float = 1800
    read_seconds: float = 180
    cleanup_seconds: float = 60
    max_bytes: int = 20 * 1024**3

    def __post_init__(self):
        for value in (self.total_seconds, self.read_seconds, self.cleanup_seconds):
            try:
                valid = type(value) in (int, float) and math.isfinite(value) and value > 0
            except OverflowError:
                valid = False
            if not valid:
                raise PreparationError("invalid_model_source")
        if type(self.max_bytes) is not int or not 0 < self.max_bytes <= 20 * 1024**3:
            raise PreparationError("invalid_model_source")
