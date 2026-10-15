"""Engine check CommandResult contract; independent of live platform backends."""

from inferyard.contracts.schemas_common import BOOL, LABEL, array, enum, nullable, obj

# Ineligibility reuses the fixed PreparationError reasons from config create;
# a passing check never confers candidate eligibility (config create decides).
REASONS = (
    "invalid_library_manifest",
    "unsupported_engine_build_requires_adapter_validation",
    "unsupported_runtime_profile",
    "runtime_receipt_invalid",
    "runtime_profile_mismatch",
    "runtime_asset_mismatch",
)
LIMITATIONS = (
    "invalid_input",
    "unsupported_platform",
    "engine_version_query_failed",
    "package_resource_invalid",
    "io_error",
    "internal_error",
    "cancelled",
)
MODES = obj({"cpu": BOOL, "metal": BOOL})
DETAILS = obj(
    {
        "eligible": BOOL,
        "reason": nullable(enum(*REASONS)),
        "reported_version": nullable(LABEL),
        "expected_release": LABEL,
        "modes": nullable(MODES),
        "mode": nullable(enum("cpu", "cuda")),
    }
)
DETAILS["allOf"] = [
    {
        "if": {"properties": {"eligible": {"const": True}}},
        "then": {
            "properties": {"reason": {"type": "null"}, "reported_version": LABEL},
            # macOS reports per-mode results (mode stays null); Windows reports
            # the receipt profile mode (modes stays null).
            "allOf": [
                {
                    "if": {"properties": {"mode": {"type": "null"}}},
                    "then": {"properties": {"modes": MODES}},
                    "else": {
                        "properties": {
                            "mode": enum("cpu", "cuda"),
                            "modes": {"type": "null"},
                        }
                    },
                }
            ],
        },
        "else": {
            "properties": {
                "reason": enum(*REASONS),
                "reported_version": {"type": "null"},
                "modes": {"type": "null"},
                "mode": {"type": "null"},
            }
        },
    }
]
ENGINE_CHECK = obj(
    {
        "command": enum("engine check"),
        "status": enum("checked", "blocked", "error", "interrupted"),
        "completeness": enum("complete", "incomplete"),
        "run_id": {"type": "null"},
        "evidence_dir": {"type": "null"},
        "limitations": array(enum(*LIMITATIONS)),
        "details": nullable(DETAILS),
    }
)
ENGINE_CHECK["allOf"] = [
    {
        "if": {"properties": {"status": {"const": "checked"}}},
        "then": {"properties": {"completeness": {"const": "complete"}, "details": DETAILS}},
        "else": {
            "properties": {"completeness": {"const": "incomplete"}, "details": {"type": "null"}}
        },
    }
]
