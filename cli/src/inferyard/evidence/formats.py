"""Current artifact boundaries, independent of evidence integrity errors."""

from inferyard.contracts.format_versions import require_current, require_version
from inferyard.errors import ContractError, EvidenceError


def require_core(value, artifact):
    require_current(value, artifact)


def require_input(value, artifact):
    try:
        require_core(value, artifact)
    except EvidenceError as exc:
        raise ContractError(artifact, "invalid schema version") from exc
