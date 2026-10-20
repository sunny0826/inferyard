"""Read-only, structured candidate presentation for CLI and native callers."""

from inferyard.config.loader import load_config
from inferyard.config.preparation_io import PreparationError
from inferyard.platforms.local_engines.assets import assets

from .launch import description


def describe(request):
    config = load_config(request.candidate).config.to_dict()
    if config.get("definition") != "local-engine-config.v1":
        raise PreparationError("local_engine_config_required")
    runtime, _, _ = assets(config)
    model, engine = config["model"], config["engine"]
    return {
        "schema_version": 3,
        "definition": "local-engine-description.v1",
        "family": engine["family"],
        "launch": description(config, runtime["installation"]),
        "assets": {
            "installation": {
                "path": engine["installation_path"],
                "sha256": engine["installation_sha256"],
            },
            "model": {
                "path": model["local_path"],
                "kind": model["kind"],
                "content_sha256": model["content_sha256"],
                "manifest_path": model["assets_path"],
                "manifest_sha256": model["assets_sha256"],
            },
            "template": {"path": model["template_path"], "sha256": model["template_sha256"]},
        },
        "generation": config["generation"],
        "conditions": config["conditions"],
        "safety": config["safety"],
        "endpoint": config["endpoint"],
        "bound": "binding_path" in config["endpoint"],
        "effective_parameters": None,
        "effective_template": None,
        "effective_unknown_reason": "requires_live_engine_observation",
        "model_requests_sent": 0,
    }
