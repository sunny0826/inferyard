"""Build canonical identity documents from actual tiny files, without native probes."""

import hashlib
from copy import deepcopy
from urllib.parse import urlsplit

from inferyard.config.local_engines.launch import startup
from inferyard.contracts.validation import json_bytes
from inferyard.platforms.identity import hash_file
from inferyard.platforms.local_engines.offline import validate_frozen_identity


def freeze(config, state, root):
    runtime_root = root / "synthetic-runtime"
    runtime_root.mkdir()
    executable = runtime_root / "python"
    executable.write_bytes(b"synthetic executable: never launched")
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
    exe_resolved = executable.resolve(strict=True)
    exe_identity = hash_file(exe_resolved)
    runtime = {
        "schema_version": 3,
        "definition": "local-engine-runtime.v1",
        "installation": declaration,
        "python": None,
        "roots": [
            {
                "path": str(exe_resolved),
                "entries": [
                    {
                        "path": exe_resolved.name,
                        "sha256": exe_identity.sha256,
                        "size": exe_identity.size,
                        "symlink": None,
                    }
                ],
            }
        ],
        "unresolved_libraries": [],
    }
    files = [exe_identity]

    def document(name, value):
        path = root / name
        path.write_bytes(json_bytes(value))
        identity = hash_file(path)
        files.append(identity)
        return str(path), identity.sha256

    engine["installation_path"], engine["installation_sha256"] = document("runtime.json", runtime)
    template_identity = hash_file(template)
    files.append(template_identity)
    config["model"].update(
        local_path=str(model),
        template_path=str(template),
        template_sha256=template_identity.sha256,
    )
    raw = b"\0".join(arg.encode() for arg in [str(executable), *engine["startup_args"]]) + b"\0"
    state.binding.update(
        executable_sha256=exe_identity.sha256,
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
    local = {"runtime": runtime, "binding": binding}
    validate_frozen_identity(config, local, files=files)
    return local, files
