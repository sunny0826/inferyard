"""Current artifact boundaries, independent of evidence integrity errors."""

from inferyard.contracts.format_versions import require_version
from inferyard.errors import ContractError, EvidenceError, UnsupportedFormat


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
    try:
        require_core(value, artifact)
    except EvidenceError as exc:
        raise ContractError(artifact, "invalid schema version") from exc
