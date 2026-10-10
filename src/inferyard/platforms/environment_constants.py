"""Copy run-start identity fields into later periodic snapshots."""

from copy import deepcopy

CONSTANT_FIELDS = (
    "platform",
    "kernel",
    "architecture",
    "cpu_model",
    "logical_cpus",
    "memory_total_bytes",
    "os_release",
    "cpu_flags",
    "boot_id",
    "page_size_bytes",
    "gpu",
    "evidence_durability",
)


def capture_constants(snapshot):
    """Keep the start snapshot's constant fields, including an observed null."""
    if not isinstance(snapshot, dict):
        return None
    return deepcopy({key: snapshot[key] for key in CONSTANT_FIELDS if key in snapshot})


def apply_constants(snapshot, constants):
    """Copy only constants present at start, including null; never invent keys."""
    if constants is None:
        return snapshot
    for key in CONSTANT_FIELDS:
        if key in constants:
            snapshot[key] = deepcopy(constants[key])
        else:
            snapshot.pop(key, None)
    return snapshot


def constant_value(constants, key, read):
    """Avoid reading constants solely to discard or overwrite them later."""
    return constants.get(key) if constants is not None else read()
