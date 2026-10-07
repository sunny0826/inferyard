"""Offline evidence reconstruction and deterministic metric reduction."""

from __future__ import annotations

from statistics import median

STATES = ("completed", "failed", "cancelled", "invalid", "not_executed")


def distribution(values, unit, excluded=0):
    return {
        "sample_count": len(values),
        "excluded": excluded,
        "unit": unit,
        "min": min(values) if values else None,
        "median": median(values) if values else None,
        "max": max(values) if values else None,
        "reason": None if values else "no_samples",
    }


def rate(numerator, denominator, excluded=0, reason=None):
    reason = reason or ("zero_denominator" if denominator == 0 else None)
    return {
        "numerator": numerator,
        "denominator": denominator,
        "excluded": excluded,
        "value": numerator / denominator if reason is None else None,
        "reason": reason,
    }
