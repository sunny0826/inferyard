"""Versioned, explicit token-budget inventory; current inventory only."""

from inferyard.evidence.engine_capabilities import observation_sealed
from inferyard.evidence.formats import require_version
from inferyard.evidence.storage import EvidenceError
from inferyard.evidence.trial_evidence import TrialEvidence

V1 = "token-budgets.json"
V2 = "token-budgets.v2.json"
DEFINITION = "token-budgets.v2"


def planned_inputs(config, selected, *, kind="run"):
    entries = [("probe", None)]
    if kind == "run":
        if config["execution"]["warmup_count"]:
            entries.append(("warmup", None))
        entries.extend(("formal", cid) for cid in selected)
    return entries


async def collect_budgets(adapter, config, bundle, selected, *, kind="run"):
    cases = {c["case_id"]: c for c in bundle["cases"]}
    entries = []
    for phase, cid in planned_inputs(config, selected, kind=kind):
        prompt = cases[cid]["prompt"] if cid is not None else config["execution"][phase + "_prompt"]
        entries.append(
            {
                "phase": phase,
                "case_id": cid,
                "budget": await adapter.token_budget(config, prompt),
            }
        )
    return {"definition": DEFINITION, "entries": entries}


def entries_v2(value):
    require_version(value, "definition", (DEFINITION,), "token-budgets")
    if (
        type(value) is not dict
        or set(value) != {"definition", "entries"}
        or value["definition"] != DEFINITION
        or type(value["entries"]) is not list
    ):
        raise EvidenceError("token_budget_definition_invalid")
    pairs = []
    for entry in value["entries"]:
        if (
            type(entry) is not dict
            or set(entry) != {"phase", "case_id", "budget"}
            or type(entry["budget"]) is not dict
        ):
            raise EvidenceError("token_budget_entry_invalid")
        phase, cid = entry["phase"], entry["case_id"]
        if phase not in ("probe", "warmup", "formal") or (
            (type(cid) is not str or not cid) if phase == "formal" else cid is not None
        ):
            raise EvidenceError("token_budget_entry_invalid")
        pairs.append((phase, cid))
    if len(set(pairs)) != len(pairs):
        raise EvidenceError("token_budget_inventory_mismatch")
    return value["entries"]


def validate_inventory(value, config, selected, *, kind="run"):
    entries = entries_v2(value)
    if [(e["phase"], e["case_id"]) for e in entries] != planned_inputs(config, selected, kind=kind):
        raise EvidenceError("token_budget_inventory_mismatch")
    return entries


def read_budgets(root, config, selected, *, kind="run", manifest=None, reads=None):
    reads = reads if reads is not None else TrialEvidence(root)
    if not reads.has_observation("token_budgets"):
        return None, None
    if manifest is not None and not observation_sealed(reads, "token_budgets", manifest):
        raise EvidenceError("token_budget_evidence_unsealed")
    value = reads.observation("token_budgets")
    validate_inventory(value, config, selected, kind=kind)
    return "token_budgets", value


def probe_budget(value):
    return entries_v2(value)[0]["budget"]


def formal_budgets(value, selected):
    entries = entries_v2(value)
    formal = [e for e in entries if e["phase"] == "formal"]
    if [e["case_id"] for e in formal] != list(selected):
        raise EvidenceError("template_token_inventory_mismatch")
    return [e["budget"] for e in formal]
