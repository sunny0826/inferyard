"""Present validated legacy candidates without probing or executing their engine."""


def describe(config):
    model, engine = config["model"], config["engine"]
    return {
        "schema_version": 3,
        "definition": "engine-description.v1",
        "adapter": engine["adapter"],
        "launch": {
            "kind": "direct_process",
            "argv": [engine["binary_path"], *engine["startup_args"]],
            # Studio's legacy startup inherits both from its own process.
            "working_directory": None,
            "environment": None,
            "ownership": "operator",
        },
        "assets": {
            "engine": {"path": engine["binary_path"], "sha256": engine["binary_sha256"]},
            "model": {
                "path": model["local_path"],
                "kind": model.get("kind", "gguf"),
                "content_sha256": model["sha256"],
            },
            "template": {"path": model["template_path"], "sha256": model["template_sha256"]},
        },
        "generation": config["generation"],
        "conditions": config["conditions"],
        "endpoint": config["endpoint"],
        "model_requests_sent": 0,
    }
