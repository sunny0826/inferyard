"""Resolve declared Python source origins without importing engine packages."""

from pathlib import Path

from inferyard.data.local_engine.bootstrap import import_paths
from inferyard.platforms.identity import PreflightError


def resolve_sources(python, declaration, roots):
    try:
        site, paths = import_paths(declaration["executable"], python["version"], python["path"])
    except (OSError, ValueError) as exc:
        raise PreflightError("local_engine_python_import_path_invalid") from exc
    if site != python["site_packages"]:
        raise PreflightError("local_engine_python_import_path_invalid")
    search = [Path(p) for p in paths]
    for directory in search:
        if not any(
            directory.resolve().is_relative_to(root.resolve()) for root in roots if root.is_dir()
        ):
            raise PreflightError("local_engine_import_root_undeclared")
    module = {
        "vllm": "vllm.entrypoints.openai.api_server",
        "sglang": "sglang.launch_server",
        "mlx-lm": "mlx_lm",
    }[declaration["family"]]
    parts, locations, origins = module.split("."), search, []
    for index, part in enumerate(parts):
        namespace = []
        selected = None
        for directory in locations:
            package = directory / part
            regular = package / "__init__.py"
            source = directory / (part + ".py")
            # Do not infer opaque extension/zip entrypoints from version strings.
            if regular.is_file():
                selected = regular
                locations = [package]
                break
            if source.is_file():
                if index != len(parts) - 1:
                    raise PreflightError("local_engine_python_entrypoint_invalid")
                selected = source
                break
            if package.is_dir():
                namespace.append(package)
        if selected is None:
            if not namespace:
                raise PreflightError("local_engine_python_entrypoint_missing")
            locations = namespace
        else:
            origins.append(str(selected.resolve()))
    python["import_roots"] = [str(path.resolve()) for path in search]
    python["entrypoint_sources"] = origins
    if not origins:
        raise PreflightError("local_engine_python_entrypoint_missing")
