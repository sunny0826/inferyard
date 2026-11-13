"""Identify offline artifacts and retain each dedicated verifier's authority."""

from dataclasses import replace
from importlib import import_module
from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.application.types import CommandRequest, CommandResult
from inferyard.contracts.validation import strict_json_loads
from inferyard.errors import ContractError, EvidenceError, UnsupportedFormat
from inferyard.evidence.storage import local_file, read_json
from inferyard.evidence.verification_options import VerificationOptions

_MARKERS = {
    "overhead": {"result.json"},
    "repeat": {"repetition-summary.json"},
    "rescore": {"rescore.json", "parent-analysis.json", "parent-lineage.json"},
    "export": {"export.json"},
    "public": {"candidate.json", "redactions.json", "REPRODUCE.md"},
    "legacy-report": {"index.json"},
    "comparison": {"comparison.json"},
    "engine-comparison": {"engine-comparison.json"},
    "extension": {"packet.json"},
}
_HANDLERS = {
    "overhead": ("inferyard.application.overhead", "overhead-check"),
    "repeat": ("inferyard.application.reporting", "repeat-check"),
    "rescore": ("inferyard.application.reporting", "rescore-check"),
    "export": ("inferyard.application.reporting", "export-check"),
    "public": ("inferyard.application.reporting", "public-check"),
    "comparison": ("inferyard.application.reporting", "compare-check"),
    "engine-comparison": (
        "inferyard.application.reporting",
        "engine-compare-check",
    ),
    "extension": ("inferyard.application.extensions", "extension-check"),
}


def verification_options(request: CommandRequest) -> VerificationOptions:
    roots = []
    for entry in request.source_roots or ():
        old, separator, new = entry.partition("=")
        if not separator or not old or not new:
            raise ContractError("source_root", "expected OLD=NEW")
        pair = (Path(old).resolve(), Path(new).resolve())
        if any(pair[0].is_relative_to(p[0]) or p[0].is_relative_to(pair[0]) for p in roots):
            raise ContractError("source_root", "overlapping source roots")
        roots.append(pair)
    return VerificationOptions(tuple(roots))


def _object(root: Path, name: str) -> dict:
    value = read_json(local_file(root, name))
    if not isinstance(value, dict):
        raise EvidenceError("invalid_artifact_marker:" + name)
    return value


def _identify(root: Path) -> set[str]:
    if root.is_file():
        return {"plan"}
    if not root.is_dir():
        raise EvidenceError("verify_requires_artifact_directory")
    names = {entry.name for entry in root.iterdir()}
    # A current Studio document is identified by file name only, owns the whole
    # directory and is never routable to a verifier.
    if "studio.json" in names:
        if names != {"studio.json"}:
            raise EvidenceError("conflicting_verify_artifacts")
        return {"studio-report"}
    # File names establish identity even when their contents are malformed.
    found = {kind for kind, markers in _MARKERS.items() if names & markers}
    fit = "checkpoint.json" in names
    if "manifest.json" in names:
        manifest = _object(root, "manifest.json")
        fit |= str(manifest.get("definition", "")).startswith("engine_fit_manifest.")
        if "evidence_layout_version" in manifest:
            found.add("run")
    if "inputs.json" in names:
        found.add("run")
    if "run.json" in names:
        run = _object(root, "run.json")
        if run.get("kind") == "extension_packet.v1":
            found.add("extension")
        elif str(run.get("definition", "")).startswith("engine_fit_run."):
            fit = True
        else:
            found.add("run")
    if fit:
        if found - {"comparison"} or "index.json" in names:
            raise EvidenceError("conflicting_verify_artifacts")
        return {"engine-fit"}
    if "manifest.json" in names:
        manifest = _object(root, "manifest.json")
        if isinstance(manifest.get("policy"), str) and manifest["policy"].startswith(
            "public-summary."
        ):
            found.add("public")
    # A retired layout may be recognized by its HTML alone; the engine
    # comparison HTML is verified by its own verifier.
    if "report.html" in names and not found & {"extension", "engine-comparison"}:
        found.add("legacy-report")
    if "batch.json" in names:
        found.add("batch")
    if "plan.json" in names and not found & {"repeat", "extension", "run", "batch"}:
        found.add("plan")
    if "analysis.json" in names and not found & {"rescore", "export"}:
        raise EvidenceError("unsupported_verify_artifact:analysis")
    if not found:
        raise EvidenceError("unsupported_verify_artifact")
    if len(found) > 1 and found != {"comparison", "legacy-report"}:
        raise EvidenceError("conflicting_verify_artifacts:" + ",".join(sorted(found)))
    return found


def _legacy_report(root: Path) -> None:
    """Reject a recognizable retired report after checking any exact old seal."""
    from inferyard.evidence.legacy_presentation_seal import legacy_index_bytes

    raw = legacy_index_bytes(root)
    if raw is None:
        raw = local_file(root, "index.json").read_bytes()
    try:
        saved = strict_json_loads(raw.decode("utf-8"))
    except (UnicodeError, ValueError) as exc:
        raise EvidenceError("invalid_json_evidence") from exc
    if type(saved) is not dict or type(saved.get("report_format_version")) is not int:
        raise EvidenceError("invalid_artifact_version")
    raise UnsupportedFormat("report", saved["report_format_version"], ())


def _check_markers(root: Path, kinds: set[str]) -> None:
    for kind in sorted(kinds - {"legacy-report", "studio-report"}):
        # Ancillary rescore/public files also signal a damaged artifact when its
        # primary marker has disappeared; the primary marker remains required.
        primary = (
            next(iter(_MARKERS[kind]))
            if len(_MARKERS[kind]) == 1
            else {
                "rescore": "rescore.json",
                "public": "candidate.json",
            }[kind]
        )
        marker = _object(root, primary)
        for version in ("schema_version", "format_version", "report_format_version"):
            if version in marker and type(marker[version]) is not int:
                raise EvidenceError("invalid_artifact_version:" + primary)
        if kind == "repeat" and (
            marker.get("schema_version") != SCHEMA_VERSION
            or marker.get("kind") != "frozen_repeat_summary.phase2.v1"
        ):
            raise EvidenceError("unsupported_repeat_format")
        if kind == "overhead" and (
            not isinstance(marker.get("protocol_sha256"), str)
            or len(marker["protocol_sha256"]) != 64
            or any(character not in "0123456789abcdef" for character in marker["protocol_sha256"])
            or marker.get("status") not in ("evaluated", "incomplete")
            or type(marker.get("passed")) is not bool
            or not isinstance(marker.get("limitations"), list)
            or any(not isinstance(value, str) for value in marker["limitations"])
        ):
            raise EvidenceError("invalid_overhead_result_marker")
    if "extension" in kinds:
        # The legacy packet verifier reads some paths directly. Check containment
        # before it reads, including unlisted payloads and child observer files.
        for path in root.rglob("*"):
            local_file(root, str(path.relative_to(root)))
        for name in ("run.json", "packet.json", "summary.json", "report.html"):
            local_file(root, name)


def _dedicated(request: CommandRequest, kind: str, *, options=None) -> tuple[int, CommandResult]:
    module, command = _HANDLERS[kind]
    if kind in {"report", "comparison", "engine-comparison"}:
        return import_module(module).execute(replace(request, command=command), options=options)
    return import_module(module).execute(replace(request, command=command))


def _public(request: CommandRequest) -> tuple[int, CommandResult]:
    source_result = None
    if request.from_run is not None or request.config is None:
        code, result = _dedicated(request, "public")
        if request.config is None:
            return code, result
        source_result = result
    # Source projection must fail before a declared configuration can match.
    module = import_module("inferyard.application.public_plan")
    code, result = module.execute(replace(request, command="public-config-check"))
    if source_result is not None:
        result = replace(
            result,
            details={**(source_result.details or {}), **(result.details or {})},
            limitations=tuple(dict.fromkeys((*source_result.limitations, *result.limitations))),
        )
    return code, result


def execute(request: CommandRequest) -> tuple[int, CommandResult]:
    if request.run is None:
        raise ContractError("verify.path", "artifact directory required")
    try:
        kinds = _identify(request.run)
    except OSError as exc:
        raise EvidenceError("unreadable_verify_artifact") from exc
    if "public" not in kinds:
        if request.config is not None:
            raise ContractError("verify.config", "only public artifacts accept configuration")
        if request.from_run is not None:
            raise ContractError("verify.source_run", "only public artifacts accept source run")
    if request.target_run is not None and "overhead" not in kinds:
        raise ContractError("verify.target_run", "only overhead artifacts accept target run")
    # A retired comparison layout keeps its comparison identity; its index only
    # marks the retired presentation files that must be rejected later.
    accepted = kinds - {"legacy-report"} if "comparison" in kinds else kinds
    if request.source_roots and not accepted <= {"comparison", "engine-comparison"}:
        raise ContractError(
            "verify.source_root", "only comparison/engine-comparison accept source roots"
        )
    options = verification_options(request)
    try:
        if kinds <= {"run", "batch", "plan", "engine-fit"}:
            from inferyard.application.raw_verification import execute as raw

            kind = next(iter(kinds))
            code, result = raw(request, kind)
            return code, replace(result, details={**(result.details or {}), "artifact_type": kind})
        if kinds == {"studio-report"}:
            raise EvidenceError("studio_report_not_verifiable")
        if kinds == {"legacy-report"}:
            _legacy_report(request.run)
        _check_markers(request.run, kinds)
        kind = "comparison" if "comparison" in kinds else next(iter(kinds))
        if kind == "public":
            code, result = _public(request)
        else:
            code, result = _dedicated(request, kind, options=options)
        details = {**(result.details or {}), "artifact_type": kind}
        return code, replace(result, command=request.command, details=details)
    except ContractError as exc:
        if options.source_roots and exc.path == "source_root":
            raise
        raise EvidenceError("invalid_verify_artifact") from exc
    except (KeyError, TypeError, AttributeError, IndexError, ValueError) as exc:
        raise EvidenceError("invalid_verify_artifact") from exc
    except (OSError, UnicodeError) as exc:
        raise EvidenceError("unreadable_verify_artifact") from exc
