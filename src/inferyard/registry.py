"""Inspectable, packaged catalogues; listing a method never claims implementation."""

from importlib.resources import files

from inferyard import SCHEMA_VERSION
from inferyard.contracts.validation import ContractError, strict_json_loads, validate_document

WINDOWS_BATCH_ADAPTERS = ("kvmem", "ninfer", "prism_llama_server_v1")


def catalogue(kind):
    if kind == "components":
        return components()
    filename = {"methods": "methods.json", "metrics": "metrics.json"}.get(kind)
    if filename is None:
        raise ContractError("catalogue.kind", "unsupported catalogue")
    document = strict_json_loads(files("inferyard").joinpath("data", filename).read_text())
    if kind == "metrics":
        for item in document["items"]:
            validate_document("metric_definition", item)
    else:
        identifiers = [item["method_id"] for item in document["items"]]
        if len(identifiers) != len(set(identifiers)):
            raise ContractError("catalogue.items", "duplicate method identifier")
    return document


def metric_definition(identifier):
    for item in catalogue("metrics")["items"]:
        if item["metric_id"] == identifier:
            return item
    raise ContractError("metric_id", "metric is not registered")


def components():
    from inferyard.analysis.scoring import CATEGORIES, SCORER_VERSION, scorer_hash

    identity = scorer_hash()
    return {
        "schema_version": SCHEMA_VERSION,
        "items": [
            *[
                {
                    "component_id": engine,
                    "kind": "adapter",
                    "status": "implemented",
                    "scope": f"Windows serial text; {kind}; lab_observation.v1 patch required",
                    "collector": "windows-memory.v1",
                    "live_verification_required": True,
                }
                for engine, kind in (("kvmem", "GGUF"), ("ninfer", "native container"))
            ],
            {
                "component_id": "prism_llama_server_v1",
                "kind": "adapter",
                "status": "implemented",
                "scope": (
                    "Linux/Windows/macOS CPU, Windows CUDA and macOS Metal; "
                    "Prism b10743-adfffbe41, one slot"
                ),
                "live_verification_required": True,
            },
            {
                "component_id": "llama_cpp_b11146_v1",
                "kind": "adapter",
                "status": "implemented",
                "scope": "Linux CPU, upstream b11146-7fe450e19, one slot; cache disabled",
                "live_verification_required": True,
            },
            {
                "component_id": "linux-resource.v2",
                "kind": "collector",
                "status": "implemented",
                "scope": (
                    "Linux memory, bound-process CPU, host swap, thermal zones and CPUFreq policies"
                ),
                "live_verification_required": True,
            },
            {
                "component_id": "windows-resource.v1",
                "kind": "collector",
                "status": "implemented",
                "scope": (
                    "Windows Prism batch available memory and bound-process RSS; "
                    "CPU, swap, temperature, frequency, GPU and energy not collected"
                ),
                "live_verification_required": True,
            },
            {
                "component_id": "macos-resource.v1",
                "kind": "collector",
                "status": "implemented",
                "scope": (
                    "macOS available memory, bound-process RSS/CPU and host swap; "
                    "temperature, frequency and GPU/energy sensors unavailable"
                ),
                "live_verification_required": True,
            },
            *[
                {
                    "component_id": identifier,
                    "kind": "extension_protocol",
                    "status": "implemented",
                    "scope": scope,
                    "live_verification_required": True,
                }
                for identifier, scope in (
                    (
                        "closed_concurrency.v1",
                        "closed load 1/2/4; separate contract from serial trials",
                    ),
                    ("native_tools.v1", "deterministic mock functions; ordinary and tool SSE"),
                    (
                        "total_observer_control.v1",
                        "full observer ABBA with independent common guardian",
                    ),
                    (
                        "same_base_quantization.v1",
                        "receipt and actual artifact verification; performance gate separate",
                    ),
                    (
                        "conditional_events.v1",
                        "true token/queue/startup event imports; no inferred events",
                    ),
                )
            ],
            {
                "component_id": "linux-memory.v1",
                "kind": "collector",
                "status": "implemented",
                "scope": "MemAvailable and process RSS; expanded CPU metrics pending",
                "live_verification_required": True,
            },
            {
                "component_id": "windows-memory.v1",
                "kind": "collector",
                "status": "implemented",
                "scope": (
                    "Windows available physical memory and bound-process working set; "
                    "single execution"
                ),
                "live_verification_required": True,
            },
            {
                "component_id": "macos-memory.v1",
                "kind": "collector",
                "status": "implemented",
                "scope": "macOS available memory estimate and bound-process RSS; single execution",
                "live_verification_required": True,
            },
            *[
                {
                    "component_id": f"{category}.{SCORER_VERSION}",
                    "kind": "scorer",
                    "status": "implemented",
                    "category": category,
                    "sha256": identity,
                    "live_verification_required": False,
                }
                for category in CATEGORIES
            ],
        ],
    }


def adapter_factory(identifier):
    from inferyard.adapters.kvmem import KVMemAdapter
    from inferyard.adapters.llama_cpp import LlamaCppAdapter
    from inferyard.adapters.ninfer import NInferAdapter
    from inferyard.adapters.prism import PrismAdapter

    factory = {
        "kvmem": KVMemAdapter,
        "ninfer": NInferAdapter,
        "prism_llama_server_v1": PrismAdapter,
        "llama_cpp_b11146_v1": LlamaCppAdapter,
    }.get(identifier)
    if factory is None:
        raise ContractError("config.engine.adapter", "adapter is not implemented")
    return factory


def collector_factory(identifier):
    from inferyard.platforms.resources_linux import ResourceSampler
    from inferyard.platforms.telemetry import Sampler

    if identifier == "linux-resource.v2":
        return ResourceSampler
    if identifier == "macos-resource.v1":
        from inferyard.platforms.resources_macos import ResourceSampler as MacResourceSampler

        return MacResourceSampler
    if identifier == "windows-resource.v1":
        from inferyard.platforms.resources_windows import (
            ResourceSampler as WindowsResourceSampler,
        )

        return WindowsResourceSampler
    if identifier not in ("linux-memory.v1", "windows-memory.v1", "macos-memory.v1"):
        raise ContractError("collector", "collector is not implemented")
    return Sampler


def registered_score(case, answer, policy):
    from inferyard.analysis.scoring import CATEGORIES, score_case

    if case["category"] not in CATEGORIES:
        raise ContractError("case.category", "scorer is not registered")
    return score_case(case, answer, policy)


def require_execution_support(plan, loaded):
    from inferyard.analysis.scoring import SCORER_VERSION

    if plan["experiment"]["definition_versions"] != {
        "measurement": "phase2.v1",
        "scoring": SCORER_VERSION,
        "comparison": "phase2.v1",
    }:
        raise ContractError("experiment.definition_versions", "unsupported execution definitions")
    for workload in plan["experiment"]["workloads"]:
        adapter_factory(loaded[workload["workload_id"]].config.to_dict()["engine"]["adapter"])


def adapter_collector_id(identifier, *, batch=False):
    """Bind the Windows lab adapters to the existing native memory collector."""
    if identifier in ("kvmem", "ninfer"):
        return "windows-memory.v1"
    if batch:
        from inferyard.platforms.resources import resource_collector_id

        return resource_collector_id()
    import platform

    return {
        "Windows": "windows-memory.v1",
        "Darwin": "macos-memory.v1",
        "Linux": "linux-memory.v1",
    }[platform.system()]
