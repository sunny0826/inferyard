"""Build canonical identity documents from actual tiny files, without native probes."""

import hashlib
from copy import deepcopy
from importlib.resources import files as package_files
from pathlib import Path
from urllib.parse import urlsplit

from inferyard.config.engine_fit_assets import model_asset_manifest
from inferyard.config.local_engines.launch import startup
from inferyard.evidence.storage import json_bytes
from inferyard.platforms.identity import hash_file
from inferyard.platforms.local_engines.installation import _snapshot
from inferyard.platforms.local_engines.offline import validate_frozen_identity


def freeze(config, state, root):
    runtime_root = root / "synthetic-runtime"
    runtime_root.mkdir()
    executable = runtime_root / "python"
    executable.write_bytes(b"synthetic executable: never launched")
    source = runtime_root / "api_server.py"
    source.write_text("# synthetic server source\n")
    model = root / "synthetic-model"
    model.mkdir()
    (model / "config.json").write_text("{}")
    (model / "weights.safetensors").write_bytes(b"synthetic weights")
    template = model / "chat_template.jinja"
    template.write_text("{{ messages }}")
    engine = config["engine"]
    engine.update(
        working_directory=str(root),
        startup_args=startup(
            {"family": engine["family"], "working_directory": str(root)},
            model,
            "fixture",
            config["conditions"]["context_size"],
            urlsplit(config["endpoint"]["url"]).port,
        )[0],
    )
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

    def document(name, value):
        path = root / name
        path.write_bytes(json_bytes(value))
        identity = hash_file(path)
        files.append(identity)
        return str(path), identity.sha256

    engine["installation_path"], engine["installation_sha256"] = document("runtime.json", runtime)
    manifest = model_asset_manifest(model)
    config["model"].update(
        local_path=str(model),
        content_sha256=manifest["sha256"],
        template_path=str(template),
        template_sha256=hash_file(template).sha256,
    )
    config["model"]["assets_path"], config["model"]["assets_sha256"] = document(
        "assets.json", manifest
    )
    files.extend(hash_file(model / entry["path"]) for entry in manifest["files"])
    raw = b"\0".join(arg.encode() for arg in [str(executable), *engine["startup_args"]]) + b"\0"
    state.binding.update(
        executable_sha256=hash_file(executable).sha256,
        argv_sha256=hashlib.sha256(raw).hexdigest(),
        cwd_sha256=hashlib.sha256(str(root).encode()).hexdigest(),
        workers=[],
    )
    state.binding["model_binding"].update(
        path=str(model), device=model.stat().st_dev, inode=model.stat().st_ino
    )
    binding = {
        "schema_version": 3,
        "definition": "local-engine-binding.v1",
        "installation_sha256": engine["installation_sha256"],
        "service": deepcopy(state.binding),
    }
    endpoint = config["endpoint"]
    endpoint["binding_path"], endpoint["binding_sha256"] = document("binding.json", binding)
    local = {"runtime": runtime, "binding": binding, "model_assets": manifest}
    validate_frozen_identity(config, local, files=files)
    return local, files
