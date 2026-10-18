"""Construct the unique speed preset using the existing plan freeze interface."""

import hashlib
from copy import deepcopy
from pathlib import Path
from tempfile import TemporaryDirectory

from inferyard.config.bundle import require_review
from inferyard.config.context_speed_profile import CASES, PROFILE, classify_workload_inputs
from inferyard.config.planning import prepare_plan, write_plan
from inferyard.config.single_plan import compile_single_plan
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import json_bytes


def experiment_for(config, bundle):
    context = config["conditions"]["context_size"]
    if context not in CASES:
        raise ContractError("context_speed.profile", "context_unsupported")
    experiment = deepcopy(compile_single_plan(config, bundle)["experiment"])
    experiment.update(profile=PROFILE, name="Context speed measurement")
    workload = experiment["workloads"][0]
    workload.update(
        workload_id="context-speed",
        repeats=5,
        output_mode="strict_fixed_length",
        protocol={"kind": "fixed", "case_ids": [CASES[context]]},
    )
    # Three preparation requests and five idle/drain allowances per trial.
    timeout = config["execution"]["timeout_seconds"]
    overhead = 3 * timeout + 5 * config["execution"]["idle_wait_seconds"]
    overhead += config["telemetry"]["baseline_seconds"]
    workload["overhead_budget_seconds"] = overhead
    experiment["budget"].update(max_requests=5, max_wall_seconds=5 * (timeout + overhead))
    classify_workload_inputs(workload, config, bundle, experiment_profile=PROFILE)
    return experiment


def freeze(loaded, destination=None):
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    require_review(bundle)
    experiment = experiment_for(config, bundle)
    workload = experiment["workloads"][0]
    # Preserve resolved host assets; only the copied bundle is package relative.
    config["bundle"]["path"] = "bundle.json"
    with TemporaryDirectory(prefix="inferyard-context-speed-") as temporary:
        root = Path(temporary)
        for kind, document in (("config", config), ("bundle", bundle)):
            raw = json_bytes(document)
            name = kind + ".json"
            (root / name).write_bytes(raw)
            workload[kind] = {"path": name, "sha256": hashlib.sha256(raw).hexdigest()}
        source = root / "preset.json"
        source.write_bytes(json_bytes(experiment))
        return prepare_plan(source)[0] if destination is None else write_plan(source, destination)
