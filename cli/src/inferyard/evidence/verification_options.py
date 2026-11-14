"""Domain options handed to report and comparison verifiers."""

from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True, slots=True)
class VerificationOptions:
    source_roots: tuple[tuple[Path, Path], ...] = ()
