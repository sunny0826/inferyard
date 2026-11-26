"""Concrete family launch descriptions, with observed LM Studio daemon identity."""

from importlib.resources import files
from pathlib import Path

from inferyard.platforms.local_engines.detect_polystrata import resolve_polystrata_server_script
from inferyard.platforms.local_engines.startup import normalize_legacy_startup_args
from inferyard.provisioning.preparation_io import PreparationError


def startup(
    installation,
    model,
    served_model,
    context,
    port,
    service_pid=None,
    *,
    polystrata_config=None,
    standalone=False,
):
    family = installation["family"]
    network = ["--host", "127.0.0.1", "--port", str(port)]
    cwd = installation["working_directory"]
    if family == "polystrata":
        if polystrata_config is None:
            raise PreparationError("polystrata_config_required")
        server_py = resolve_polystrata_server_script(Path(cwd)) or (
            Path(cwd) / "serve" / "server.py"
        )
        return [
            str(server_py),
            "--engine",
            "strata",
            "--config",
            str(polystrata_config),
            *network,
        ], cwd
    if family == "lmstudio":
        if standalone:
            raise PreparationError("serve_family_unsupported")
        if service_pid is None:
            raise PreparationError("local_engine_lmstudio_background_pid_required")
        from inferyard.platforms.engine_fit_macos import service_process_state, psutil_module

        state = service_process_state(service_pid, psutil_module())
        if service_process_state(service_pid, psutil_module()) != state:
            raise PreparationError("service_identity_changed")
        return state[1], str(state[4])
    if family == "llama-cpp":
        if standalone:
            return [
                "-m",
                str(model),
                "-ngl",
                "99",
                "-c",
                str(context),
                "--metrics",
                "--alias",
                served_model,
                *network,
            ], cwd
        return [
            "-m",
            str(model),
            "-ngl",
            "99",
            "-c",
            str(context),
            "-np",
            "1",
            "--metrics",
            "--no-context-shift",
            "--alias",
            served_model,
            *network,
        ], cwd
    if family == "mlx-lm":
        if standalone:
            raise PreparationError("serve_family_unsupported")
        entry = str(files("inferyard").joinpath("data/local_engine/mlx_server.py"))
        prefix = [entry]
        args = ["--model", str(model), "--max-model-len", str(context)]
    elif family == "vllm":
        prefix = ["-m", "vllm.entrypoints.openai.api_server"]
        args = [
            "--model",
            str(model),
            "--max-model-len",
            str(context),
            *([] if standalone else ["--max-num-seqs", "1"]),
            "--gpu-memory-utilization",
            "0.20",
        ]
    elif family == "sglang":
        prefix = ["-m", "sglang.launch_server"]
        args = [
            "--model-path",
            str(model),
            "--context-length",
            str(context),
            *([] if standalone else ["--max-running-requests", "1"]),
            "--enable-metrics",
            "--disable-cuda-graph",
            "--mem-fraction-static",
            "0.15",
            "--sleep-on-idle",
        ]
    else:
        raise PreparationError("local_engine_family_unsupported")
    return [*prefix, *args, "--served-model-name", served_model, *network], cwd


def description(config, installation):
    engine = config["engine"]
    common = {
        "working_directory": engine["working_directory"],
        "environment": engine["environment"],
        "ownership": "operator",
    }
    if engine["family"] == "lmstudio":
        return {
            "kind": "lmstudio",
            **common,
            "lms_executable": installation["executable"],
            "models_root": installation["models_root"],
            "background_argv": engine["startup_args"],
            "served_model": engine["served_model"],
            "requires_loaded_instance": True,
        }
    normalized_args = normalize_legacy_startup_args(
        engine["family"], list(engine["startup_args"])
    )
    return {
        "kind": "direct_process",
        **common,
        "argv": [installation["executable"], *normalized_args],
    }
