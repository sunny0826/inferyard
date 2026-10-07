"""Static engine capabilities: missing observation is not hardware incompatibility."""


def capabilities():
    from inferyard.config.engine_fit_assets import mlx_server_path

    return {
        "schema_version": 3,
        "definition": "engine_fit_capabilities.v1",
        "diagnostic": True,
        "performance_comparison_qualified": False,
        "engines": [
            {
                "id": "vllm",
                "platforms": ["Linux", "Darwin"],
                "model_kind": "directory",
                "execution": "available",
                "idle_source": "/metrics",
            },
            {
                "id": "sglang",
                "platforms": ["Linux", "Darwin"],
                "model_kind": "directory",
                "execution": "available",
                "idle_source": "/metrics",
            },
            {
                "id": "llama-cpp",
                "platforms": ["Linux", "Darwin", "Windows"],
                "model_kind": "gguf",
                "execution": "available",
                "idle_source": "/metrics",
                "requirement": "externally_started_single_model_llama_server_with_metrics",
            },
            {
                "id": "mlx-lm",
                "platforms": ["Darwin"],
                "model_kind": "directory",
                "execution": "controlled_service_required",
                "idle_source": "/metrics",
                "server_script": str(mlx_server_path()),
                "requirement": "operator_starts_installed_script_in_separate_mlx_environment",
            },
            {
                "id": "lmstudio",
                "platforms": ["Darwin"],
                "model_kind": "gguf",
                "execution": "local_cli_observer_required",
                "idle_source": "lms:ps",
                "requirement": "explicit_lms_path_models_root_one_local_instance_link_disabled",
            },
            {
                "id": "ollama",
                "platforms": [],
                "model_kind": "gguf",
                "execution": "blocked",
                "idle_source": None,
                "reason": "ollama_service_idle_observation_unavailable",
                "scope": "tool_observation_gap_not_hardware_or_model_incompatibility",
            },
        ],
    }
