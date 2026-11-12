"""Current artifact version boundaries shared by producers and readers."""

from inferyard.errors import EvidenceError, UnsupportedFormat


def require_version(value, key, supported, artifact):
    expected_type = type(supported[0])
    if type(value) is not dict or type(value.get(key)) is not expected_type:
        raise EvidenceError("invalid_artifact_version")
    if value[key] not in supported:
        raise UnsupportedFormat(artifact, value[key], supported)
