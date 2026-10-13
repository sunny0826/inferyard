"""Read independent baseline sensor sidecars without granting run qualification."""

from pathlib import Path

from inferyard.evidence.storage import EvidenceError
from inferyard.evidence.trial_reads import TrialReads

_RUN_FILES = frozenset(
    {
        "manifest.json",
        "run.json",
        "inputs.json",
        "observations.jsonl",
        "request-bodies.jsonl",
        "events.jsonl",
        "memory.jsonl",
        "plan.json",
        "selection.json",
        "config.frozen.json",
        "config.input.json",
        "config.input.toml",
        "bundle.json",
        "migration.json",
        "requests.jsonl",
    }
)


def _has_run_inventory(root):
    return any(
        path.name in _RUN_FILES or path.name.endswith(".request.json") for path in root.iterdir()
    )


class BaselineEvidence:
    """Read-only legacy sensor protocol; no inputs, requests, or run identity."""

    _observations = {
        ("environment", "start"): "environment.start.json",
        ("environment", "end"): "environment.end.json",
        ("collector", "prepare"): "collector.json",
        ("boundary_observer", "prepare"): "boundary-observer.json",
    }
    _logs = frozenset(
        {"environment.jsonl", "schedule.jsonl", "external-cpu.jsonl", "request-environment.jsonl"}
    )

    def __init__(self, root):
        self.root = Path(root).resolve()
        if _has_run_inventory(self.root):
            raise EvidenceError("baseline_evidence_contains_run")
        self._reads = TrialReads(self.root)

    def _observation_name(self, kind, stage, key):
        if stage is None and kind in ("collector", "boundary_observer"):
            stage = "prepare"
        name = self._observations.get((kind, stage))
        if name is None or key is not None:
            raise EvidenceError("invalid_baseline_observation_query")
        return name

    def observation(self, kind, stage=None, key=None):
        name = self._observation_name(kind, stage, key)
        return self._reads.checked_json(name) if self._reads.exists(name) else None

    def has_observation(self, kind, stage=None, key=None):
        return self.observation(kind, stage, key) is not None

    def observation_reference(self, kind, stage=None, key=None):
        if self.observation(kind, stage, key) is None:
            return None
        name = self._observation_name(kind, stage, key)
        return {"path": name, "sha256": self._reads.hashes[name]}

    def checked_jsonl(self, name):
        if name not in self._logs:
            raise EvidenceError("invalid_baseline_log_query")
        return self._reads.checked_jsonl(name)

    def log_reference(self, name):
        self.checked_jsonl(name)
        return {"path": name, "sha256": self._reads.hashes[name]}


def environment_reads(root):
    """Identify run inventory strictly; otherwise read the independent sensor protocol."""
    from inferyard.evidence.trial_evidence import TrialEvidence

    root = Path(root).resolve()
    return TrialEvidence(root) if _has_run_inventory(root) else BaselineEvidence(root)
