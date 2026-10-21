"""Concrete family launch descriptions, with observed LM Studio daemon identity."""

from importlib.resources import files

from inferyard.config.preparation_io import PreparationError


def startup(installation, model, served_model, context, port, service_pid=None):
    family = installation["family"]
    network = ["--host", "127.0.0.1", "--port", str(port)]
    cwd = installation["working_directory"]
    if family == "lmstudio":
        if service_pid is None:
            raise PreparationError("local_engine_lmstudio_background_pid_required")
        from inferyard.platforms.engine_fit_macos import _state, psutil_module

        state = _state(service_pid, psutil_module())
        if _state(service_pid, psutil_module()) != state:
            raise PreparationError("service_identity_changed")
        return state[1], str(state[4])
    if family == "llama-cpp":
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
    entry = str(files("inferyard").joinpath("data/local_engine/bootstrap.py"))
    prefix = ["-I", "-S", "-B", entry, family]
    if family == "mlx-lm":
        args = ["--model", str(model), "--max-model-len", str(context)]
    elif family == "vllm":
        args = [
            "--model",
            str(model),
            "--max-model-len",
            str(context),
            "--max-num-seqs",
            "1",
            "--gpu-memory-utilization",
            "0.20",
        ]
    elif family == "sglang":
        args = [
            "--model-path",
            str(model),
            "--context-length",
            str(context),
            "--max-running-requests",
            "1",
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
    return {
        "kind": "direct_process",
        **common,
        "argv": [installation["executable"], *engine["startup_args"]],
    }
