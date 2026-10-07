"""Offline declared configuration match; runtime preflight is still required."""

from inferyard.application.types import CommandResult
from inferyard.config.public_recipe import fingerprint
from inferyard.config.startup_portability import portable_startup
from inferyard.evidence.storage import EvidenceError, local_file, read_json
from inferyard.reporting.public_package import verify_public

REQUIRED = {
    "generation.stop",
    "execution.probe_prompt",
    "execution.warmup_prompt",
    "conditions.background_load",
    "conditions.profile",
    "conditions.governor",
    "conditions.epp",
}


def check_config(root, loaded):
    verify_public(root)
    candidate = read_json(local_file(root, "candidate.json"))
    config = loaded.config.to_dict()
    reproduction = candidate["reproduction"]
    mismatches = []

    def check(label, observed, expected):
        if observed != expected:
            mismatches.append(label)

    for name, section, key in (
        ("model_sha256", "model", "sha256"),
        ("template_sha256", "model", "template_sha256"),
        ("engine_sha256", "engine", "binary_sha256"),
    ):
        check(name, config[section][key], candidate["identities"][name])
    check(
        "bundle_sha256",
        fingerprint(loaded.bundle.to_dict()),
        candidate["identities"]["bundle_sha256"],
    )
    for section in ("generation", "conditions"):
        for key, value in candidate["recipe"][section].items():
            check(section + "." + key, config[section].get(key), value)
    for section in ("generation", "conditions", "execution", "telemetry"):
        for key, value in reproduction[section].items():
            if key == "stop_sequences_empty":
                check("generation.stop_sequences_empty", config["generation"]["stop"] == [], value)
            elif value is not None:
                check(section + "." + key, config[section].get(key), value)
    for field, expected in reproduction["locally_required_sha256"].items():
        if field not in REQUIRED:
            raise EvidenceError("unknown_public_required_field")
        section, key = field.split(".")
        check(field, fingerprint(config[section][key]), expected)
    check("startup", portable_startup(config), reproduction["startup"])
    return {
        "declared_configuration_matches": not mismatches,
        "mismatched_fields": sorted(set(mismatches)),
        "artifact_bytes_verified": False,
        "runtime_verified": False,
        "ready_to_run": False,
        "limitations": [
            "freeze_plan_and_runtime_preflight_required",
            "device_equivalence_not_verified",
        ],
    }


def execute(request):
    result = check_config(request.run, request.config)
    passed = result["declared_configuration_matches"]
    return (0 if passed else 3), CommandResult(
        request.command,
        "matched" if passed else "mismatch",
        "incomplete",
        details=result,
        limitations=tuple(result["limitations"]),
    )
