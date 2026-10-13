"""Select requirements by saved platform; absence never selects a weaker policy."""

LINUX_FIELDS = (
    "ac_online",
    "profile",
    "governor",
    "epp",
    "scaling_driver",
    "kernel",
    "architecture",
    "cpu_model",
    "logical_cpus",
    "memory_total_bytes",
)
MACOS_FIELDS = (
    "platform",
    "ac_online",
    "profile",
    "macos_power_policy",
    "kernel",
    "architecture",
    "cpu_model",
    "logical_cpus",
    "memory_total_bytes",
)


def fields(snapshots, *, include_policy=False):
    native = any(s.get("platform") == "Darwin" for s in snapshots)
    if native:
        return MACOS_FIELDS
    return (*LINUX_FIELDS, "cpu_policies") if include_policy else LINUX_FIELDS
