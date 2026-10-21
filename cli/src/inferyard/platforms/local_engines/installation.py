"""Complete declared runtime inventories, including bytecode and link targets."""

import os
import platform
import stat
import subprocess
from pathlib import Path

from inferyard.contracts.engine_profiles import DIRECTORY_FAMILIES
from inferyard.contracts.local_environment import validate_installation_environment
from inferyard.contracts.schemas_local_engines import INSTALLATION
from inferyard.contracts.validation import _validate, strict_json_loads
from inferyard.data.local_engine.bootstrap import import_paths
from inferyard.platforms.identity import PreflightError, hash_file

RUNTIME = "local-engine-runtime.v1"
_FILE_CACHE = {}


def darwin():
    if platform.system() != "Darwin" or platform.machine() != "arm64":
        raise PreflightError("local_engine_requires_darwin_arm64")


def read_json(path):
    try:
        return strict_json_loads(Path(path).read_text(encoding="utf-8"))
    except (OSError, ValueError, UnicodeError) as exc:
        raise PreflightError("local_engine_json_unreadable") from exc


def description(value):
    _validate(value, INSTALLATION, "installation")
    result = dict(value)
    for key in ("executable", "working_directory", "models_root"):
        if key in result:
            path = Path(result[key])
            if not path.is_absolute():
                raise PreflightError("local_engine_absolute_path_required")
            # Preserve launcher symlinks: venv prefix depends on their lexical path.
            result[key] = (
                os.path.normpath(str(path))
                if key == "executable"
                else str(path.resolve(strict=True))
            )
            if not path.exists():
                raise PreflightError("local_engine_locator_missing")
    roots = [Path(value) for value in result["runtime_roots"]]
    if any(not p.is_absolute() or not p.is_dir() or p.is_symlink() for p in roots):
        raise PreflightError("local_engine_runtime_root_invalid")
    result["runtime_roots"] = sorted({str(p.resolve()) for p in roots})
    if not Path(result["working_directory"]).is_dir():
        raise PreflightError("local_engine_cwd_invalid")
    if not Path(result["executable"]).is_file():
        raise PreflightError("local_engine_executable_invalid")
    if (result["family"] == "lmstudio") != ("models_root" in result):
        raise PreflightError("local_engine_models_root_required_for_lmstudio_only")
    validate_installation_environment(result)
    return result


def read_installation(path):
    try:
        return description(read_json(path))
    except OSError as exc:
        raise PreflightError("local_engine_installation_unreadable") from exc


def inventory(root):
    def stamp(path, info, target):
        if not stat.S_ISREG(target.st_mode):
            raise PreflightError("local_engine_non_regular_asset")
        return (
            info.st_dev,
            info.st_ino,
            info.st_size,
            info.st_mtime_ns,
            info.st_ctime_ns,
            target.st_dev,
            target.st_ino,
            target.st_size,
            target.st_mtime_ns,
            target.st_ctime_ns,
            os.readlink(path) if stat.S_ISLNK(info.st_mode) else None,
        )

    if not root.is_dir():
        return {root.name: stamp(root, root.lstat(), root.stat())}
    if root.is_symlink():
        raise PreflightError("local_engine_directory_symlink")
    result, pending = {}, [(str(root), "")]
    while pending:
        directory, prefix = pending.pop()
        with os.scandir(directory) as entries:
            for entry in entries:
                info = entry.stat(follow_symlinks=False)
                name = prefix + entry.name
                if entry.is_dir():
                    if entry.is_symlink():
                        raise PreflightError("local_engine_directory_symlink")
                    pending.append((entry.path, name + "/"))
                else:
                    result[name] = stamp(entry.path, info, entry.stat())
    return result


def _python(description):
    executable = Path(description["executable"])
    if not (executable.parent.parent / "pyvenv.cfg").is_file():
        raise PreflightError("local_engine_python_venv_required")
    # -S prevents site/.pth execution; only Python's stdlib is imported.
    code = (
        "import sys,sysconfig,json;print(json.dumps({'path':sys.path,"
        "'prefix':sys.prefix,'base_prefix':sys.base_prefix,"
        "'paths':sysconfig.get_paths(),'version':list(sys.version_info[:2])}))"
    )
    try:
        before = hash_file(executable)
        result = subprocess.run(
            [str(executable), "-I", "-S", "-B", "-c", code],
            capture_output=True,
            text=True,
            timeout=10,
            env={"PATH": "/usr/bin:/bin", "HOME": str(Path.home())},
            cwd=description["working_directory"],
        )
        if result.returncode or len(result.stdout) > 65536:
            raise PreflightError("local_engine_python_probe_failed")
        hash_file(executable, before.sha256)
        value = strict_json_loads(result.stdout)
        venv = executable.parent.parent
        site, import_roots = import_paths(executable, value["version"], value["path"])
        roots = [Path(p) for p in import_roots if Path(p).exists()]
        roots += [site, venv / "pyvenv.cfg", Path(value["base_prefix"]) / "lib"]
        value["site_packages"] = site
        return value, [Path(p) for p in roots]
    except (OSError, ValueError, KeyError, subprocess.TimeoutExpired) as exc:
        raise PreflightError("local_engine_python_probe_failed") from exc


def _roots(declaration, python_roots):
    paths = [Path(p) for p in declaration["runtime_roots"]]
    paths += [Path(declaration["executable"]), *python_roots]
    # Keep lexical launcher and resolved target as separate assets.
    paths += [Path(declaration["executable"]).resolve(strict=True)]
    if declaration["family"] in DIRECTORY_FAMILIES:
        from importlib.resources import files

        paths.append(Path(str(files("inferyard").joinpath("data/local_engine/bootstrap.py"))))
    if declaration["family"] == "mlx-lm":
        paths += [
            Path(str(files("inferyard").joinpath("data/local_engine/mlx_server.py"))),
            Path(str(files("inferyard").joinpath("data/engine_fit/mlx_server.py"))),
        ]
    selected = []
    for path in sorted(set(paths), key=lambda p: (len(p.parts), str(p))):
        if not any(parent.is_dir() and path.is_relative_to(parent) for parent in selected):
            selected.append(path)
    return selected


def _snapshot(roots):
    snapshots, identities = [], []
    for root in roots:
        before = inventory(root)
        directory = root.is_dir()
        entries = []
        for name, stamps in sorted(before.items()):
            path = root / name if directory else root
            cache_key = (str(path), stamps)
            identity = _FILE_CACHE.get(cache_key)
            if identity is None:
                identity = hash_file(path)
                _FILE_CACHE[cache_key] = identity
            identities.append(identity)
            entries.append(
                {
                    "path": name,
                    "sha256": identity.sha256,
                    "size": identity.size,
                    "symlink": stamps[-1],
                }
            )
        if inventory(root) != before:
            raise PreflightError("local_engine_runtime_changed_during_hash")
        snapshots.append({"path": str(root), "entries": entries})
    return snapshots, identities


def _freeze(value):
    darwin()
    try:
        declaration = description(value)
        python, paths = (None, [])
        if declaration["family"] in DIRECTORY_FAMILIES:
            python, paths = _python(declaration)
        roots = _roots(declaration, paths)
        if python is not None:
            from inferyard.platforms.local_engines.python_sources import resolve_sources

            resolve_sources(python, declaration, roots)
        snapshots, identities = _snapshot(roots)
        from inferyard.platforms.local_engines.closure import verify_libraries

        unresolved = verify_libraries(
            [Path(item.path) for item in identities], Path(declaration["executable"])
        )
        return {
            "schema_version": 3,
            "definition": RUNTIME,
            "installation": declaration,
            "python": python,
            "roots": snapshots,
            "unresolved_libraries": unresolved,
        }, identities
    except (OSError, UnicodeError) as exc:
        raise PreflightError("local_engine_runtime_unreadable") from exc


def freeze_installation(value):
    return _freeze(value)[0]


def verify_installation(snapshot):
    darwin()
    if (
        not isinstance(snapshot, dict)
        or set(snapshot)
        != {
            "schema_version",
            "definition",
            "installation",
            "python",
            "roots",
            "unresolved_libraries",
        }
        or snapshot["schema_version"] != 3
        or snapshot["definition"] != RUNTIME
    ):
        raise PreflightError("local_engine_runtime_invalid")
    from inferyard.platforms.local_engines.runtime_schema import RUNTIME as schema

    _validate(snapshot, schema, "local_engine_runtime")
    current, identities = _freeze(snapshot["installation"])
    if current != snapshot:
        raise PreflightError("local_engine_runtime_changed")
    return identities
