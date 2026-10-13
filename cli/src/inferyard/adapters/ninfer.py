"""Native NInfer container adapter with discovered observation capabilities."""

from inferyard.adapters.lab_adapter import LabAdapter


class NInferAdapter(LabAdapter):
    engine_id = "ninfer"
