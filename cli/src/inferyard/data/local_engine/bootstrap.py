"""Run only the formal engine entrypoints without executing Python site hooks."""

import runpy
import sys
from pathlib import Path

MODULES = {
    "vllm": "vllm.entrypoints.openai.api_server",
    "sglang": "sglang.launch_server",
}


def import_paths(executable, version, initial):
    """Derive the same lexical venv paths for inspection and live execution."""
    launcher = Path(executable)
    if not launcher.is_absolute():
        raise ValueError("local_engine_python_venv_required")
    venv = launcher.parent.parent
    config = venv / "pyvenv.cfg"
    if not config.is_file():
        raise ValueError("local_engine_python_venv_required")
    values = {}
    for line in config.read_text(encoding="utf-8").splitlines():
        key, separator, value = line.partition("=")
        if separator:
            key = key.strip().lower()
            if key in values:
                raise ValueError("local_engine_python_venv_config_ambiguous")
            values[key] = value.strip().lower()
    if values.get("include-system-site-packages") != "false":
        raise ValueError("local_engine_system_site_packages_rejected")
    site = venv / "lib" / f"python{version[0]}.{version[1]}" / "site-packages"
    if not site.is_dir() or site.is_symlink():
        raise ValueError("local_engine_python_site_packages_required")
    paths = []
    for item in [*initial, str(site)]:
        path = Path(item)
        if not item or not path.is_absolute():
            raise ValueError("local_engine_python_import_path_invalid")
        normalized = str(path.resolve())
        if normalized not in paths:
            paths.append(normalized)
    return str(site), paths


def main():
    if not (sys.flags.isolated and sys.flags.no_site and sys.flags.dont_write_bytecode):
        raise ValueError("local_engine_python_isolation_required")
    if len(sys.argv) < 2 or sys.argv[1] not in {*MODULES, "mlx-lm"}:
        raise ValueError("local_engine_direct_family_invalid")
    _, paths = import_paths(sys.executable, sys.version_info[:2], sys.path)
    sys.path[:] = paths
    family, arguments = sys.argv[1], sys.argv[2:]
    if family == "mlx-lm":
        script = Path(__file__).with_name("mlx_server.py")
        sys.argv = [str(script), *arguments]
        runpy.run_path(str(script), run_name="__main__")
    else:
        sys.argv = [MODULES[family], *arguments]
        runpy.run_module(MODULES[family], run_name="__main__", alter_sys=True)


if __name__ == "__main__":
    main()
