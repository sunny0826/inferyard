"""Exception categories shared across InferYard layers.

Only the standard library may be imported here, so leaf modules and the CLI
entry point can describe failures without loading heavier subsystems.
"""

from __future__ import annotations


class PreflightError(RuntimeError):
    """Fixed error category only; callers must not persist raw OS exceptions."""


class EvidenceError(RuntimeError):
    pass


class UnsupportedFormat(RuntimeError):
    def __init__(self, artifact, saved, supported):
        self.artifact = artifact
        self.saved = saved
        self.supported = list(supported)
        super().__init__("unsupported_format")


class ContractError(ValueError):
    """Only field paths and fixed descriptions; never include rejected values."""

    def __init__(self, path: str, reason: str):
        self.path = path
        self.reason = reason
        super().__init__(f"{path}: {reason}")
