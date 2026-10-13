"""KVMem GGUF adapter with discovered lab or native observation capabilities."""

from inferyard.adapters.lab_adapter import LabAdapter


class KVMemAdapter(LabAdapter):
    engine_id = "kvmem"
