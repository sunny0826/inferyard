"""Periodic persistence only; live and endpoint snapshots stay complete (ADR 041)."""

_OMITTED_FIELDS = frozenset(
    {"cpu_flags", "os_release", "gpu", "mem_available_bytes", "page_size_bytes"}
)


def periodic_environment(snapshot: dict) -> dict:
    """Keep identity, policy, counters and source/read metadata without mutating input."""
    return {key: value for key, value in snapshot.items() if key not in _OMITTED_FIELDS}


class EnvironmentSidecars:
    """Independent sensor sidecars, without run identities or run qualification."""

    _names = {
        ("environment", "start"): "environment.start.json",
        ("environment", "end"): "environment.end.json",
        ("collector", None): "collector.json",
        ("boundary_observer", None): "boundary-observer.json",
    }

    def __init__(self, root):
        from inferyard.evidence.trial_reads import TrialReads

        self.reads = TrialReads(root)

    def observation(self, kind, stage=None, key=None):
        from inferyard.evidence.storage import EvidenceError

        name = self._names.get((kind, stage))
        if name is None or key is not None:
            raise EvidenceError("invalid_environment_sidecar_query")
        return self.reads.checked_json(name) if self.reads.exists(name) else None

    def has_observation(self, kind, stage=None, key=None):
        return self.observation(kind, stage, key) is not None

    def observation_reference(self, kind, stage=None, key=None):
        if self.observation(kind, stage, key) is None:
            return None
        name = self._names[kind, stage]
        return {"path": name, "sha256": self.reads.hashes[name]}

    def checked_jsonl(self, name):
        return self.reads.checked_jsonl(name)

    def log_reference(self, name):
        self.checked_jsonl(name)
        return {"path": name, "sha256": self.reads.hashes[name]}


def environment_reads(root):
    """Keep standalone sensor analysis separate from strict run layout recognition."""
    from inferyard.evidence.trial_evidence import TrialEvidence

    run_files = {
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
        "bundle.json",
        "migration.json",
        "requests.jsonl",
    }
    if any(
        path.name in run_files or path.name.endswith(".request.json") for path in root.iterdir()
    ):
        return TrialEvidence(root)
    return EnvironmentSidecars(root)
