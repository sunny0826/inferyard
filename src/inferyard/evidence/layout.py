"""Strict layout detection and private layout 1 name mapping."""

from inferyard.evidence.formats import UnsupportedFormat, require_core
from inferyard.evidence.request_snapshots import require_current_sources
from inferyard.evidence.storage import EvidenceError

INPUT_FILES = {
    "run": "run.json",
    "plan": "plan.json",
    "selection": "selection.json",
    "config": "config.frozen.json",
    "bundle": "bundle.json",
}
OBSERVATION_FILES = {
    ("parent", "prepare"): "parent.json",
    ("service_binding", "prepare"): "service-binding.json",
    ("service_reuse", "prepare"): "service-reuse.json",
    ("safety_policy", "prepare"): "safety-policy.json",
    ("environment", "start"): "environment.start.json",
    ("environment", "end"): "environment.end.json",
    ("service_properties", None): "service.props.json",
    ("engine_capabilities", "pre_idle"): "engine-capabilities.json",
    ("recovery", "post_idle"): "recovery.json",
    ("token_budgets", "prepare"): "token-budgets.v2.json",
    ("input_target_check", "prepare"): "input-target-check.json",
    ("collector", "prepare"): "collector.json",
    ("observer", "prepare"): "observer.json",
    ("boundary_observer", "prepare"): "boundary-observer.json",
    ("effective_parameters", "ordinary"): "effective-ordinary.json",
    ("effective_parameters", "stream"): "effective-stream.json",
    ("identity", None): "identity.json",
    ("safety_check", "finalizing"): "safety-check.json",
    ("execution_budget", "finalizing"): "execution-budget.json",
}
BASE_FILES = {
    "inputs.json",
    "observations.jsonl",
    "request-bodies.jsonl",
    "events.jsonl",
    "memory.jsonl",
    "environment.jsonl",
    "schedule.jsonl",
}


def identify(reads):
    names = {path.name for path in reads.root.iterdir()} | set(
        (reads.manifest or {}).get("files", {})
    )
    require_current_sources(reads.root, names)
    declared = (reads.manifest or {}).get("evidence_layout_version")
    if "inputs.json" in names:
        envelope = reads.checked_json("inputs.json")
        if type(envelope) is not dict or type(envelope.get("evidence_layout_version")) is not int:
            raise EvidenceError("invalid_evidence_layout")
        version = envelope["evidence_layout_version"]
        if reads.manifest is not None and (type(declared) is not int or declared != version):
            raise EvidenceError("evidence_layout_conflict")
        if version != 2:
            raise UnsupportedFormat("run.evidence_layout_version", version, (1, 2))
        legacy = set(INPUT_FILES.values()) | set(OBSERVATION_FILES.values())
        if names & legacy or any(name.endswith(".request.json") for name in names):
            raise EvidenceError("mixed_evidence_layout")
        if names & {"config.input.json", "config.input.toml"} or any(
            name.startswith("client-http-clean-") for name in names
        ):
            raise EvidenceError("mixed_evidence_layout")
        if reads.manifest is not None and not BASE_FILES.issubset(reads.manifest["files"]):
            raise EvidenceError("incomplete_layout_manifest")
        return 2
    if declared is not None:
        if type(declared) is not int:
            raise EvidenceError("invalid_evidence_layout")
        if declared != 2:
            raise UnsupportedFormat("run.evidence_layout_version", declared, (1, 2))
        raise EvidenceError("evidence_layout_conflict")
    if names & {"observations.jsonl", "request-bodies.jsonl"}:
        raise EvidenceError("mixed_evidence_layout")
    if "run.json" not in names:
        raise EvidenceError("run_evidence_missing")
    require_core(reads.checked_json("run.json"), "run")
    return 1
