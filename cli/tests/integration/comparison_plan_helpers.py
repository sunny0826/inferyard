"""One local synthetic plan executes all 12 slots and reads back as v2 slots."""

import asyncio
import hashlib
import json
import os
import struct
import shutil
from collections.abc import Callable
from copy import deepcopy
from dataclasses import dataclass, replace
from importlib.resources import files as package_files
from pathlib import Path
from types import SimpleNamespace
from typing import TypedDict
from urllib.parse import urlsplit


from inferyard.adapters.local_engines import LocalEngineAdapter
from inferyard.analysis.comparison_types import JSONRecord, JSONValue
from inferyard.analysis.engine_comparison_v2 import read_comparison_v2
from inferyard.application.types import CommandRequest
from inferyard.config.comparison_plan import Slot, comparison_slots, freeze
from inferyard.config.engine_fit_assets import model_asset_manifest
from inferyard.config.local_engines.launch import startup
from inferyard.config.loader import load_config
from inferyard.config.toml_writer import render
from inferyard.errors import PreflightError
from inferyard.analysis.ledger import read_trial
from inferyard.contracts.validation import json_bytes
from inferyard.platforms.identity import FileIdentity, environment_snapshot, hash_file
from inferyard.platforms.local_engines.installation import _snapshot
from inferyard.platforms.local_engines.offline import validate_frozen_identity
from inferyard.runtime import batch_state
from inferyard.registry import adapter_collector_id, adapter_factory, collector_factory
from inferyard.application.execution import execute_async
from inferyard.runtime.trial_runner import TrialDependencies
from tests.unit.test_comparison_plan import (
    bundle_document,
    extraction_case,
    instruction_case,
    svg_case,
    write_bundle,
    write_config,
)


Scenario = tuple[CommandRequest, TrialDependencies, list[JSONRecord], JSONRecord]
RigFactory = Callable[[str], SimpleNamespace]




def _write_identity(
    root: Path, name: str, value: JSONRecord, files: list[FileIdentity]
) -> tuple[str, str]:
    path = root / name
    path.write_bytes(json_bytes(value))
    identity = hash_file(path)
    files.append(identity)
    return str(path), identity.sha256


def _runtime_declaration(
    engine: JSONRecord, executable: Path, root: Path, runtime_root: Path
) -> JSONRecord:
    return {
        "schema_version": 3,
        "definition": "local-engine-installation.v1",
        "family": engine["family"],
        "executable": str(executable),
        "working_directory": str(root),
        "runtime_roots": [str(runtime_root)],
        "environment": engine["environment"],
    }


def _runtime_python(runtime_root: Path, source: Path) -> JSONRecord:
    return {
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
    }


def _synthetic_runtime(
    root: Path, engine: JSONRecord
) -> tuple[JSONRecord, list[FileIdentity], Path, Path]:
    root.mkdir(parents=True, exist_ok=True)
    runtime_root = root / "synthetic-runtime"
    runtime_root.mkdir()
    executable = runtime_root / "python"
    executable.write_bytes(b"synthetic executable: never launched")
    source = runtime_root / "api_server.py"
    source.write_text("# synthetic server source\n")
    template = root / "chat_template.jinja"
    template.write_text("{{ messages }}")
    declaration = _runtime_declaration(engine, executable, root, runtime_root)
    bootstrap = Path(str(package_files("inferyard").joinpath("data/local_engine/bootstrap.py")))
    roots, runtime_files = _snapshot([runtime_root, bootstrap])
    runtime = {
        "schema_version": 3,
        "definition": "local-engine-runtime.v1",
        "installation": declaration,
        "python": _runtime_python(runtime_root, source),
        "roots": roots,
        "unresolved_libraries": [],
    }
    return runtime, list(runtime_files), executable, template


def _bind_engine(
    config: JSONRecord, root: Path, model: Path, template: Path
) -> JSONRecord:
    engine = config["engine"]
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
    manifest = model_asset_manifest(model)
    config["model"].update(
        kind="gguf",
        local_path=str(model),
        content_sha256=manifest["sha256"],
        template_path=str(template),
        template_sha256=hash_file(template).sha256,
    )
    return manifest


def _bind_state(
    state: SimpleNamespace, root: Path, model: Path, executable: Path, engine: JSONRecord
) -> None:
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


def freeze_gguf(
    config: JSONRecord, state: SimpleNamespace, root: Path, model: Path
) -> tuple[JSONRecord, list[FileIdentity]]:
    engine = config["engine"]
    engine.update(working_directory=str(root))
    runtime, files, executable, template = _synthetic_runtime(root, engine)
    manifest = _bind_engine(config, root, model, template)
    engine["installation_path"], engine["installation_sha256"] = _write_identity(
        root, "runtime.json", runtime, files
    )
    config["model"]["assets_path"], config["model"]["assets_sha256"] = _write_identity(
        root, "assets.json", manifest, files
    )
    files.append(hash_file(model))
    files.append(hash_file(template))
    _bind_state(state, root, model, executable, engine)
    binding = {
        "schema_version": 3,
        "definition": "local-engine-binding.v1",
        "installation_sha256": engine["installation_sha256"],
        "service": deepcopy(state.binding),
    }
    config["endpoint"]["binding_path"], config["endpoint"]["binding_sha256"] = _write_identity(
        root, "binding.json", binding, files
    )
    local = {"runtime": runtime, "binding": binding, "model_assets": manifest}
    validate_frozen_identity(config, local, files=files)
    return local, files

@dataclass
class SideSetup:
    root: Path
    tmp_path: Path
    bundles: dict[str, JSONRecord]
    bundle_paths: dict[str, Path]
    shared_model: Path


def _side_inputs(
    setup: SideSetup, side: str, family: str, state: SimpleNamespace
) -> tuple[list[Path], dict[str, JSONRecord], dict[str, list[FileIdentity]]]:
    configs, identities, identity_files = [], {}, {}
    for key, bundle in setup.bundles.items():
        config = load_config(
            write_config(
                setup.root,
                f"{side}-{key}-seed",
                {
                    "bundle_path": setup.bundle_paths[key],
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
            config, state, setup.tmp_path / "identity" / side / key, setup.shared_model
        )
        identities[key] = local
        identity_files[key] = files
        path = setup.root / f"{side}-{key}.toml"
        path.write_text(render(config))
        configs.append(path)
    return configs, identities, identity_files


def local_inputs(
    tmp_path: Path, rig: Callable[[str], SimpleNamespace]
) -> tuple[
    JSONRecord,
    dict[str, list[Path]],
    dict[str, SimpleNamespace],
    dict[tuple[str, str], JSONRecord],
    dict[tuple[str, str], list[FileIdentity]],
]:
    root = tmp_path / "inputs"
    root.mkdir()
    bundles = {
        "core": bundle_document("core", [instruction_case(), extraction_case()]),
        "pelican": bundle_document("pelican", [svg_case()]),
    }
    bundle_paths = {key: write_bundle(root, key, bundle) for key, bundle in bundles.items()}
    states = {"a": rig("llama-cpp"), "b": rig("lmstudio")}
    shared_model = tmp_path / "synthetic.gguf"
    shared_model.write_bytes(b"GGUF" + struct.pack("<IQQ", 3, 1, 0) + b"synthetic tensor bytes")
    setup = SideSetup(root, tmp_path, bundles, bundle_paths, shared_model)
    identities, identity_files, sides = {}, {}, {}
    for side, family in (("a", "llama-cpp"), ("b", "lmstudio")):
        configs, side_identities, side_files = _side_inputs(
            setup, side, family, states[side]
        )
        sides[side] = configs
        identities.update({(side, key): value for key, value in side_identities.items()})
        identity_files.update({(side, key): value for key, value in side_files.items()})
    plan = freeze(
        sides["a"],
        sides["b"],
        [str(path) for path in bundle_paths.values()],
        destination=tmp_path / "plan",
    )
    return plan, sides, states, identities, identity_files

class ControlledSafety:
    def __init__(self, policy: JSONRecord, config: JSONRecord, environment: JSONRecord) -> None:
        self.policy = policy
        self.config = config
        self.environment = environment

    def metadata(self) -> JSONRecord:
        return {"policy": self.policy, "sensors": [], "scope": "controlled_test"}

    def check(self, *, periodic: bool = False) -> None:
        if periodic:
            return None


def slot_inputs(
    plan: JSONRecord,
    identities: dict[tuple[str, str], JSONRecord],
    tmp_path: Path,
) -> tuple[tuple[Slot, ...], dict[str, tuple[Path, int, int, str, JSONRecord]]]:
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


@dataclass
class ExecutionCursor:
    state: SimpleNamespace
    identity: JSONRecord
    files: list[FileIdentity]
    service: JSONRecord
    adapter: LocalEngineAdapter | None = None


@dataclass
class SlotExecution:
    plan: JSONRecord
    states: dict[str, SimpleNamespace]
    identities: dict[tuple[str, str], JSONRecord]
    identity_files: dict[tuple[str, str], list[FileIdentity]]
    bindings: dict[str, tuple[Path, int, int, str, JSONRecord]]
    deps: TrialDependencies
    request: CommandRequest
    cursor: ExecutionCursor
    alive: dict[int, int]

    def process_start(self, pid: int) -> int:
        if pid in self.alive:
            return self.alive[pid]
        raise PreflightError("service_process_unavailable")

    def fresh_adapter(self, state: SimpleNamespace, family: str) -> LocalEngineAdapter:
        return LocalEngineAdapter(
            self.cursor.service["origin"],
            transport=state.adapter.client._transport,
            binding_verifier=lambda _: deepcopy(self.cursor.service),
            observer=state.observer if family == "lmstudio" else None,
        )

    def adapter(self, origin: str, **_: JSONValue) -> LocalEngineAdapter | None:
        assert origin
        return self.cursor.adapter

    def preflight(self, _: JSONRecord) -> tuple[JSONRecord, list[FileIdentity]]:
        return (
            {
                "origin": self.cursor.service["origin"],
                "environment": environment_snapshot(),
                "verification": "verified",
                "local_engine": self.cursor.identity,
            },
            self.cursor.files,
        )

    def advance(self, index: int, trial: JSONRecord) -> None:
        path, pid, ticks, family, binding = self.bindings[trial["trial_id"]]
        key = "core" if trial["workload_id"].endswith("core") else "pelican"
        self.cursor.state = self.states[family]
        self.cursor.identity = deepcopy(self.identities[(family, key)])
        self.cursor.identity["binding"] = deepcopy(binding)
        self.cursor.files = [*self.identity_files[(family, key)], hash_file(path)]
        self.cursor.service = deepcopy(binding["service"])
        self.cursor.adapter = self.fresh_adapter(
            self.cursor.state, self.cursor.state.config["engine"]["family"]
        )
        self.alive.clear()
        self.alive[pid] = ticks
        self.request = replace(
            self.request,
            workload_id=trial["workload_id"],
            endpoint_url=self.cursor.service["origin"],
            server_pid=pid,
            service_binding_path=path,
            handoff_note=None if index == 0 else "synthetic operator handoff",
        )

    def run_all(self) -> None:
        for index, trial in enumerate(self.plan["trials"]):
            self.advance(index, trial)
            code, result = asyncio.run(execute_async(self.request, self.deps))
            if index < len(self.plan["trials"]) - 1:
                assert code == 3 and result.status == "awaiting_handoff", result
                next_workload = self.plan["trials"][index + 1]["workload_id"]
                assert result.details["next_trial"]["workload_id"] == next_workload
            else:
                assert code == 0 and result.status == "finished", result


def assert_family_dispatch(sides: dict[str, list[Path]]) -> None:
    for side in ("a", "b"):
        config = load_config(sides[side][0]).config.to_dict()
        assert adapter_factory(config["engine"]["adapter"]) is LocalEngineAdapter
        assert collector_factory(adapter_collector_id(config["engine"]["adapter"], batch=True))
    assert load_config(sides["a"][0]).config.to_dict()["engine"]["family"] == "llama-cpp"
    assert load_config(sides["b"][0]).config.to_dict()["engine"]["family"] == "lmstudio"




def assert_read_back(tmp_path: Path, slots: tuple[Slot, ...]) -> None:
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
