"""Current artifact boundaries, independent of evidence integrity errors."""

from inferyard.evidence.storage import EvidenceError


class UnsupportedFormat(RuntimeError):
    def __init__(self, artifact, saved, supported):
        self.artifact = artifact
        self.saved = saved
        self.supported = list(supported)
        super().__init__("unsupported_format")


def require_version(value, key, supported, artifact):
    expected_type = type(supported[0])
    if type(value) is not dict or type(value.get(key)) is not expected_type:
        raise EvidenceError("invalid_artifact_version")
    if value[key] not in supported:
        raise UnsupportedFormat(artifact, value[key], supported)


def require_core(value, artifact):
    require_version(value, "schema_version", (3,), artifact)
    if value.get("origin") == "migrated":
        raise UnsupportedFormat(artifact + ".origin", "migrated", ("measured",))


def check_presentation_seal(root):
    from inferyard.evidence.artifact_seal import read_sealed
    from inferyard.evidence.storage import local_file

    if local_file(root, "artifact-manifest.json").exists():
        names = ["index.json", "report.html"]
        if local_file(root, "comparison.json").exists():
            names.append("comparison.json")
        read_sealed(root, names)


def require_input(value, artifact):
    from inferyard.contracts.validation import ContractError

    try:
        require_core(value, artifact)
    except EvidenceError as exc:
        raise ContractError(artifact, "invalid schema version") from exc
