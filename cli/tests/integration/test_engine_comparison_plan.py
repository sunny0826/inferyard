"""One local synthetic plan executes all 12 slots and reads back as v2 slots."""

import asyncio
import hashlib
import json
import os
import struct
import shutil
from importlib.resources import files as package_files
from copy import deepcopy
from dataclasses import replace
from pathlib import Path
from urllib.parse import urlsplit

import pytest

from inferyard.adapters.local_engines import LocalEngineAdapter
from inferyard.analysis.engine_comparison_v2 import read_comparison_v2
from inferyard.application.types import CommandRequest
from inferyard.config.comparison_plan import comparison_slots, freeze
from inferyard.config.engine_fit_assets import model_asset_manifest
from inferyard.config.local_engines.launch import startup
from inferyard.config.loader import load_config
from inferyard.config.toml_writer import render
from inferyard.evidence.storage import json_bytes
from inferyard.platforms.identity import PreflightError, environment_snapshot, hash_file
from inferyard.platforms.local_engines.installation import _snapshot
from inferyard.platforms.local_engines.offline import validate_frozen_identity
from inferyard.runtime import batch_state
from inferyard.registry import adapter_collector_id, adapter_factory, collector_factory
from inferyard.runtime.batch_runner import execute_async
from inferyard.runtime.trial_runner import TrialDependencies
import tests.local_engine_transport.conftest as local_engine_conftest
from tests.integration.test_runner import scenario

__all__ = ["scenario"]
from tests.unit.test_comparison_plan import (
    bundle_document,
    extraction_case,
    instruction_case,
    svg_case,
    write_bundle,
    write_config,
)


@pytest.fixture(name="batch_scenario")
def batch_scenario(scenario: object) -> object:
    return scenario


@pytest.fixture(name="rig")
def rig_fixture() -> object:
    return local_engine_conftest.rig.__wrapped__()


def freeze_gguf(config: dict, state: object, root: Path, model: Path) -> tuple[dict, list]:
    root.mkdir(parents=True, exist_ok=True)
    runtime_root = root / "synthetic-runtime"
    runtime_root.mkdir()
    executable = runtime_root / "python"
    executable.write_bytes(b"synthetic executable: never launched")
    source = runtime_root / "api_server.py"
    source.write_text("# synthetic server source\n")
    template = root / "chat_template.jinja"
    template.write_text("{{ messages }}")
    engine = config["engine"]
    engine.update(working_directory=str(root))
    if engine["family"] == "llama-cpp":
        engine["startup_args"] = startup(
            {"family": engine["family"], "working_directory": str(root)},
            model,
            engine["served_model"],
            config["conditions"]["context_size"],
            urlsplit(config["endpoint"]["url"]).port,
        )[0]
    else:
        engine["startup_args"] = []
    declaration = {
        "schema_version": 3,
        "definition": "local-engine-installation.v1",
        "family": engine["family"],
        "executable": str(executable),
        "working_directory": str(root),
        "runtime_roots": [str(runtime_root)],
        "environment": engine["environment"],
    }
    bootstrap = Path(str(package_files("inferyard").joinpath("data/local_engine/bootstrap.py")))
    roots, runtime_files = _snapshot([runtime_root, bootstrap])
    runtime = {
        "schema_version": 3,
        "definition": "local-engine-runtime.v1",
        "installation": declaration,
        "python": {
            "path": [str(runtime_root)],
            "prefix": str(runtime_root),
            "base_prefix": str(runtime_root),
            "paths": dict.fromkeys(
                (
                    "stdlib",
                    "platstdlib",
                    "purelib",
                    "platlib",
                    "include",
                    "platinclude",
                    "scripts",
                    "data",
                ),
                str(runtime_root),
            ),
            "version": [3, 15],
            "site_packages": str(runtime_root),
            "import_roots": [str(runtime_root)],
            "entrypoint_sources": [str(source)],
        },
        "roots": roots,
        "unresolved_libraries": [],
    }
    files = list(runtime_files)

    def document(name: str, value: dict) -> tuple[str, str]:
        path = root / name
        path.write_bytes(json_bytes(value))
        identity = hash_file(path)
        files.append(identity)
        return str(path), identity.sha256

    engine["installation_path"], engine["installation_sha256"] = document("runtime.json", runtime)
    manifest = model_asset_manifest(model)
    config["model"].update(
        kind="gguf",
        local_path=str(model),
        content_sha256=manifest["sha256"],
        template_path=str(template),
        template_sha256=hash_file(template).sha256,
    )
    config["model"]["assets_path"], config["model"]["assets_sha256"] = document(
        "assets.json", manifest
    )
    files.append(hash_file(model))
    files.append(hash_file(template))
    arguments = (
        engine["startup_args"]
        if engine["family"] == "lmstudio"
        else [str(executable), *engine["startup_args"]]
    )
    raw = b"\0".join(os.fsencode(arg) for arg in arguments) + b"\0"
    state.binding.update(
        executable_sha256=hash_file(executable).sha256,
        argv_sha256=hashlib.sha256(raw).hexdigest(),
        cwd_sha256=hashlib.sha256(os.fsencode(str(root))).hexdigest(),
        workers=[],
    )
    state.binding["model_binding"].update(
        engine=engine["family"],
        path=str(model),
        device=model.stat().st_dev,
        inode=model.stat().st_ino,
        source="lms_loaded_instance_path"
        if engine["family"] == "lmstudio"
        else "verified_startup_file_identity",
    )
    binding = {
        "schema_version": 3,
        "definition": "local-engine-binding.v1",
        "installation_sha256": engine["installation_sha256"],
        "service": deepcopy(state.binding),
    }
    config["endpoint"]["binding_path"], config["endpoint"]["binding_sha256"] = document(
        "binding.json", binding
    )
    local = {"runtime": runtime, "binding": binding, "model_assets": manifest}
    validate_frozen_identity(config, local, files=files)
    return local, files


def local_inputs(tmp_path: Path, rig: object) -> tuple[dict, dict, dict, dict, dict]:
    root = tmp_path / "inputs"
    root.mkdir()
    bundles = {
        "core": bundle_document("core", [instruction_case(), extraction_case()]),
        "pelican": bundle_document("pelican", [svg_case()]),
    }
    bundle_paths = {
        key: write_bundle(root, key, bundle) for key, bundle in bundles.items()
    }
    states = {"a": rig("llama-cpp"), "b": rig("lmstudio")}
    shared_model = tmp_path / "synthetic.gguf"
    shared_model.write_bytes(b"GGUF" + struct.pack("<IQQ", 3, 1, 0) + b"synthetic tensor bytes")
    identities = {}
    identity_files = {}
    sides = {}
    for side, family in (("a", "llama-cpp"), ("b", "lmstudio")):
        state = states[side]
        configs = []
        for key, bundle in bundles.items():
            config = load_config(
                write_config(
                    root,
                    f"{side}-{key}-seed",
                    {
                        "bundle_path": bundle_paths[key],
                        "bundle": bundle,
                        "family": family,
                    },
                )
            ).config.to_dict()
            config["endpoint"].update(
                url=state.binding["origin"],
                server_pid=state.binding["pid"],
                process_start_ticks=state.binding["start_ticks"],
            )
            local, files = freeze_gguf(
                config, state, tmp_path / "identity" / side / key, shared_model
            )
            identities[(side, key)] = local
            identity_files[(side, key)] = files
            path = root / f"{side}-{key}.toml"
            path.write_text(render(config))
            configs.append(path)
        sides[side] = configs
    plan = freeze(
        sides["a"],
        sides["b"],
        [str(path) for path in bundle_paths.values()],
        destination=tmp_path / "plan",
    )
    return plan, sides, states, identities, identity_files


class ControlledSafety:
    def __init__(self, policy: dict, config: dict, environment: dict) -> None:
        self.policy = policy

    def metadata(self) -> dict:
        return {"policy": self.policy, "sensors": [], "scope": "controlled_test"}

    def check(self, *, periodic: bool = False) -> None:
        return None


def slot_inputs(
    plan: dict, sides: dict, identities: dict, tmp_path: Path
) -> tuple[tuple, dict]:
    slots = comparison_slots(plan)
    bindings = {}
    for index, slot in enumerate(slots):
        family = "a" if slot.side == "a" else "b"
        base = deepcopy(identities[(slot.side, slot.bundle_key)]["binding"])
        pid, ticks = 200_000 + index, 500 + index
        base["service"].update(pid=pid, start_ticks=ticks, workers=[])
        path = tmp_path / "bindings" / f"{index}.json"
        path.parent.mkdir(exist_ok=True)
        path.write_bytes(json_bytes(base))
        bindings[slot.trial_id] = (path, pid, ticks, family, base)
    return slots, bindings


def test_twelve_slots_execute_with_handoffs_and_read_back(
    tmp_path: Path,
    batch_scenario: object,
    rig: object,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _, deps, calls, _ = batch_scenario
    plan, sides, states, identities, identity_files = local_inputs(tmp_path, rig)
    slots, bindings = slot_inputs(plan, sides, identities, tmp_path)
    assert len(slots) == 12
    for side in ("a", "b"):
        config = load_config(sides[side][0]).config.to_dict()
        assert adapter_factory(config["engine"]["adapter"]) is LocalEngineAdapter
        assert collector_factory(adapter_collector_id(config["engine"]["adapter"], batch=True))
    assert load_config(sides["a"][0]).config.to_dict()["engine"]["family"] == "llama-cpp"
    assert load_config(sides["b"][0]).config.to_dict()["engine"]["family"] == "lmstudio"

    alive = {}
    current = {
        "state": states["a"],
        "identity": identities[("a", "core")],
        "files": identity_files[("a", "core")],
        "service": identities[("a", "core")]["binding"]["service"],
    }

    def process_start(pid: int) -> int:
        if pid in alive:
            return alive[pid]
        raise PreflightError("service_process_unavailable")

    monkeypatch.setattr(batch_state, "process_start_ticks", process_start)
    def fresh_adapter(state: object, family: str) -> LocalEngineAdapter:
        return LocalEngineAdapter(
            current["service"]["origin"],
            transport=state.adapter.client._transport,
            binding_verifier=lambda _: deepcopy(current["service"]),
            observer=state.observer if family == "lmstudio" else None,
        )

    def preflight(config: dict) -> tuple[dict, list]:
        return (
            {
                "origin": current["service"]["origin"],
                "environment": environment_snapshot(),
                "verification": "verified",
                "local_engine": current["identity"],
            },
            current["files"],
        )

    deps.preflight = preflight
    deps.adapter = lambda origin, secret: current["adapter"]
    deps.safety = ControlledSafety
    request = CommandRequest(
        "run",
        frozen_plan=tmp_path / "plan" / "plan.json",
        output_root=tmp_path / "batch",
    )
    for index, trial in enumerate(plan["trials"]):
        path, pid, ticks, family, binding = bindings[trial["trial_id"]]
        current["state"] = states[family]
        key = "core" if trial["workload_id"].endswith("core") else "pelican"
        current["identity"] = deepcopy(identities[(family, key)])
        current["identity"]["binding"] = deepcopy(binding)
        current["files"] = [*identity_files[(family, key)], hash_file(path)]
        current["service"] = deepcopy(binding["service"])
        current["adapter"] = fresh_adapter(
            current["state"], current["state"].config["engine"]["family"]
        )
        alive.clear()
        alive[pid] = ticks
        request = replace(
            request,
            workload_id=trial["workload_id"],
            endpoint_url=current["service"]["origin"],
            server_pid=pid,
            service_binding_path=path,
            handoff_note=None if index == 0 else "synthetic operator handoff",
        )
        code, result = asyncio.run(execute_async(request, deps))
        if index < len(plan["trials"]) - 1:
            assert code == 3 and result.status == "awaiting_handoff", result
            assert (
                result.details["next_trial"]["workload_id"]
                == plan["trials"][index + 1]["workload_id"]
            )
        else:
            assert code == 0 and result.status == "finished", result
    before = len(calls)
    code, again = asyncio.run(execute_async(request, deps))
    assert code == 0 and again.status == "finished"
    assert len(calls) == before
    assert len(list((tmp_path / "batch" / "runs").iterdir())) == 12

    document = read_comparison_v2(tmp_path / "plan", tmp_path / "batch")
    assert document["eligible"] is True, document["diagnostics"]
    assert document["run_set"]["expected_trials"] == [slot.trial_id for slot in slots]
    assert len(document["run_set"]["observed_runs"]) == 12
    assert document["run_set"]["missing"] == []
    assert document["run_set"]["unplanned"] == []
    assert document["history_stop"] is None
    assert {row["status"] for row in document["handoff"]["slots"]} == {"ok"}
    assert len(document["bundles"]) == 2
    assert all(
        len(group["left"]) == 3 and len(group["right"]) == 3
        for group in document["bundles"].values()
    )
    assert_drift_and_endpoint_rejection(tmp_path, request, deps)


def _rehash_manifest(run_dir: Path, filename: str) -> None:
    manifest_path = run_dir / "manifest.json"
    manifest = json.loads(manifest_path.read_text())
    raw = (run_dir / filename).read_bytes()
    entry = manifest["files"][filename]
    entry["sha256"] = hashlib.sha256(raw).hexdigest()
    entry["bytes"] = len(raw)
    manifest_path.write_bytes(json_bytes(manifest))


def _mutate_config_drift(run_dir: Path) -> None:
    inputs_path = run_dir / "inputs.json"
    inputs = json.loads(inputs_path.read_text())
    config = inputs["documents"]["config"]["payload"]
    config["device"]["id"] = "LAB-DRIFTED"
    payload = json_bytes(config)
    inputs["documents"]["config"] = {
        "payload": config,
        "sha256": hashlib.sha256(payload).hexdigest(),
    }
    selection = inputs["documents"]["selection"]["payload"]
    selection["config_sha256"] = inputs["documents"]["config"]["sha256"]
    inputs["documents"]["selection"]["sha256"] = hashlib.sha256(
        json_bytes(selection)
    ).hexdigest()
    inputs_path.write_bytes(json_bytes(inputs))
    _rehash_manifest(run_dir, "inputs.json")


def _mutate_endpoint(run_dir: Path) -> None:
    path = run_dir / "observations.jsonl"
    rows = [json.loads(line) for line in path.read_text().splitlines()]
    for row in rows:
        if row["kind"] == "service_binding":
            row["payload"]["effective_endpoint"]["server_pid"] += 1
            row["payload_sha256"] = hashlib.sha256(json_bytes(row["payload"])).hexdigest()
            break
    path.write_text("".join(json.dumps(row) + "\n" for row in rows))
    _rehash_manifest(run_dir, "observations.jsonl")


def assert_drift_and_endpoint_rejection(
    tmp_path: Path,
    request: CommandRequest,
    deps: TrialDependencies,
) -> None:
    batch = tmp_path / "batch"
    drift = tmp_path / "drift-batch"
    shutil.copytree(batch, drift)
    run_dir = next((drift / "runs").iterdir())
    _mutate_config_drift(run_dir)
    rejected = read_comparison_v2(tmp_path / "plan", drift)
    assert rejected["eligible"] is False
    assert any(
        row["detail"] == "batch_frozen_inputs_changed" for row in rejected["diagnostics"]
    )
    code, stopped = asyncio.run(
        execute_async(replace(request, output_root=drift), deps)
    )
    assert code == 3 and stopped.status == "comparison_history_stopped"
    assert stopped.details["comparison_stop"]["reason"] == "comparison_history_identity_error"
    resume = CommandRequest("resume", from_run=run_dir)
    code, stopped = asyncio.run(execute_async(resume, deps))
    assert code == 3 and stopped.status == "comparison_history_stopped"

    endpoint = tmp_path / "endpoint-batch"
    shutil.copytree(batch, endpoint)
    endpoint_run = next((endpoint / "runs").iterdir())
    _mutate_endpoint(endpoint_run)
    rejected = read_comparison_v2(tmp_path / "plan", endpoint)
    assert rejected["eligible"] is False
    assert any(
        row["detail"] == "service_binding_differs_from_execution"
        for row in rejected["diagnostics"]
    )
