"""Raw cumulative platform counters with an explicit collector definition."""

from copy import deepcopy

from inferyard.contracts.schemas_common import LABEL, NAT, POS, VERSION, enum, nullable, obj
from inferyard.contracts.schemas_memory import MEMORY_SAMPLE

RESOURCE_SAMPLE = deepcopy(MEMORY_SAMPLE)
RESOURCE_SAMPLE["properties"].update(
    {
        "schema_version": VERSION,
        "collector": enum("linux-resource.v2", "macos-resource.v1"),
        "metric_name": enum("service_cpu_ticks", "system_swap_in", "system_swap_out"),
        "unit": enum("ticks", "pages"),
        "boot_id": nullable(LABEL),
        "clock_ticks_per_second": nullable(POS),
        "page_size_bytes": nullable(POS),
        "raw_cpu": nullable(obj({"user_ticks": NAT, "system_ticks": NAT})),
        "capture": enum("periodic", "request_start", "request_end", "idle"),
    }
)
RESOURCE_SAMPLE["required"] = list(RESOURCE_SAMPLE["properties"])


def validate_resource_sample(data):
    from inferyard.contracts.validation import ContractError, _sample_invariants

    _sample_invariants(data)
    cpu = data["metric_name"] == "service_cpu_ticks"
    if data["collector"] == "macos-resource.v1":
        sources = {
            "service_cpu_ticks": "psutil:Process.cpu_times:user+system",
            "system_swap_in": "vm_stat:Swapins",
            "system_swap_out": "vm_stat:Swapouts",
        }
        if data["source"] != sources[data["metric_name"]]:
            raise ContractError("sample.source", "macOS collector source mismatch")
        if cpu and data["clock_ticks_per_second"] not in (None, 1_000_000):
            raise ContractError("sample.clock_ticks_per_second", "macOS CPU uses microsecond ticks")
    if data["value"] is None and data["raw_cpu"] is not None:
        raise ContractError("sample.raw_cpu", "missing counter cannot retain valid CPU components")
    if data["unit"] != ("ticks" if cpu else "pages"):
        raise ContractError("sample.unit", "counter unit mismatch")
    if data["value"] is not None:
        if data["boot_id"] is None:
            raise ContractError("sample.boot_id", "counter requires boot identity")
        if cpu:
            if data["server_pid"] is None or data["process_start_ticks"] is None:
                raise ContractError("sample.server_pid", "CPU requires process identity")
            if data["clock_ticks_per_second"] is None or data["raw_cpu"] is None:
                raise ContractError("sample.raw_cpu", "CPU counter scale and components required")
            if data["value"] != sum(data["raw_cpu"].values()):
                raise ContractError("sample.value", "CPU counter components differ")
        elif data["page_size_bytes"] is None:
            raise ContractError("sample.page_size_bytes", "swap byte conversion scale required")
    if cpu and data["page_size_bytes"] is not None:
        raise ContractError("sample.page_size_bytes", "CPU is not a page counter")
    if not cpu and any(
        data[k] is not None
        for k in ("server_pid", "process_start_ticks", "raw_cpu", "clock_ticks_per_second")
    ):
        raise ContractError("sample.server_pid", "host swap is not process attribution")
