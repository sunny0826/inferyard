"""Complete declared runtime inventories, including bytecode and link targets."""

import json
import os
import platform
import stat
import subprocess
import tempfile
from pathlib import Path

from inferyard.contracts.engine_profiles import DIRECTORY_FAMILIES
from inferyard.contracts.local_environment import validate_installation_environment
from inferyard.contracts.schemas_local_engines import INSTALLATION
from inferyard.contracts.validation import strict_json_loads, validate_value
from inferyard.data.local_engine.bootstrap import import_paths
from inferyard.errors import PreflightError
from inferyard.platforms.identity import FileIdentity, hash_file

RUNTIME = "local-engine-runtime.v1"
_FILE_CACHE = {}
_DISK_CACHE_DEFINITION = "runtime-hash-cache.v1"
_MAX_DISK_CACHE_BYTES = 32 * 1024 * 1024
_MAX_DISK_CACHE_ENTRIES = 50_000
_HEX_LOWER = frozenset("0123456789abcdef")


def _hash_cache_path() -> Path:
    return Path.home() / ".cache" / "inferyard" / "runtime-hash-cache.v1.json"


def _load_disk_cache() -> dict:
    try:
        path = _hash_cache_path()
        if path.is_symlink() or (path.parent.exists() and path.parent.is_symlink()):
            return {}
        info = path.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_size > _MAX_DISK_CACHE_BYTES:
            return {}
        data = strict_json_loads(path.read_text(encoding="utf-8"))
        if (
            not isinstance(data, dict)
            or data.get("definition") != _DISK_CACHE_DEFINITION
            or not isinstance(data.get("entries"), dict)
        ):
            return {}
        return dict(data["entries"])
    except (OSError, ValueError, TypeError, RuntimeError):
        return {}


def _save_disk_cache(entries: dict) -> None:
    tmp_path = None
    try:
        path = _hash_cache_path()
        cache_dir = path.parent
        if path.is_symlink() or (cache_dir.exists() and cache_dir.is_symlink()):
            return
        cache_dir.mkdir(parents=True, exist_ok=True)
        if len(entries) > _MAX_DISK_CACHE_ENTRIES:
            trimmed = dict(list(entries.items())[-_MAX_DISK_CACHE_ENTRIES:])
        else:
            trimmed = entries
        payload = json.dumps(
            {"definition": _DISK_CACHE_DEFINITION, "entries": trimmed},
            ensure_ascii=False,
            allow_nan=False,
            separators=(",", ":"),
        )
        fd, tmp_name = tempfile.mkstemp(
            dir=cache_dir, prefix="runtime-hash-cache-", suffix=".tmp"
        )
        tmp_path = Path(tmp_name)
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            stream.write(payload)
            stream.flush()
        os.chmod(tmp_path, 0o600)
        os.replace(tmp_path, path)
        tmp_path = None
    except (OSError, ValueError, TypeError, RuntimeError):
        if tmp_path is not None:
            try:
                tmp_path.unlink(missing_ok=True)
            except OSError:
                pass


def _lookup_disk_cache(entries: dict, path: Path, stamps: tuple) -> FileIdentity | None:
    try:
        entry = entries.get(str(path))
        if not isinstance(entry, list) or len(entry) != 7:
            return None
        dev, ino, size, mtime_ns, ctime_ns, resolved, digest = entry
        if (
            type(dev) is not int
            or type(ino) is not int
            or type(size) is not int
            or type(mtime_ns) is not int
            or type(ctime_ns) is not int
            or type(resolved) is not str
            or type(digest) is not str
            or len(digest) != 64
            or not set(digest) <= _HEX_LOWER
        ):
            return None
        st = path.stat()
        if not stat.S_ISREG(st.st_mode):
            return None
        disk_stamp = (st.st_dev, st.st_ino, st.st_size, st.st_mtime_ns, st.st_ctime_ns)
        if disk_stamp != stamps[5:10] or disk_stamp != (dev, ino, size, mtime_ns, ctime_ns):
            return None
        if str(path.resolve(strict=True)) != resolved:
            return None
        return FileIdentity(resolved, digest, st.st_size, st.st_dev, st.st_ino, st.st_mtime_ns)
    except (OSError, ValueError, TypeError, RuntimeError):
        return None


_POLYSTRATA_ENGINE_DIRS = (
    "terminal/engine",
    "terminal/engine-cuda12",
    "terminal/engine-poly",
    "engine/build-metal",
    "engine/build-vision",
    "engine",
    "engine-poly",
    "build-metal",
    "build-vision",
)
_POLYSTRATA_TOP_FILES = ("BUILD.json", "polystrata.py", "setup.py", "terminal/setup.py")


def darwin():
    if platform.system() != "Darwin" or platform.machine() != "arm64":
        raise PreflightError("local_engine_requires_darwin_arm64")


def _normalized_platform() -> tuple[str, str]:
    system = platform.system()
    machine = platform.machine().lower()
    arch = {"amd64": "x64", "x86_64": "x64", "arm64": "arm64", "aarch64": "arm64"}.get(
        machine, machine
    )
    return system, arch


def check_family_platform(family: str | None) -> tuple[str, str]:
    if family == "polystrata":
        current = _normalized_platform()
        if current not in (("Darwin", "arm64"), ("Linux", "x64"), ("Windows", "x64")):
            raise PreflightError("local_engine_platform_unsupported")
        return current
    darwin()
    return _normalized_platform()


def read_json(path):
    try:
        return strict_json_loads(Path(path).read_text(encoding="utf-8"))
    except (OSError, ValueError, UnicodeError) as exc:
        raise PreflightError("local_engine_json_unreadable") from exc


def description(value):
    validate_value(value, INSTALLATION, "installation")
    result = dict(value)
    for key in ("executable", "working_directory", "models_root", "data_dir"):
        if key in result:
            path = Path(result[key])
            if not path.is_absolute():
                raise PreflightError("local_engine_absolute_path_required")
            if not path.exists():
                raise PreflightError("local_engine_locator_missing")
            result[key] = (
                os.path.normpath(str(path))
                if key == "executable"
                else str(path.resolve(strict=True))
            )
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
    if "data_dir" in result:
        if result["family"] != "polystrata":
            raise PreflightError("local_engine_data_dir_polystrata_only")
        if not Path(result["data_dir"]).is_dir():
            raise PreflightError("local_engine_data_dir_invalid")
    validate_installation_environment(result)
    return result


def _verify_polystrata_layout(declaration: dict) -> None:
    from inferyard.platforms.local_engines.detect_polystrata import validate_polystrata_layout

    validate_polystrata_layout(
        Path(declaration["working_directory"]),
        Path(declaration["executable"]),
    )


def read_installation(path):
    try:
        return description(read_json(path))
    except OSError as exc:
        raise PreflightError("local_engine_installation_unreadable") from exc


def _allowed_bases(root, allowed_roots):
    bases = allowed_roots if allowed_roots is not None else (root,)
    resolved = []
    for item in bases:
        try:
            resolved.append(Path(item).resolve(strict=True))
        except OSError:
            continue
    return tuple(dict.fromkeys(resolved))


def inventory(root, allowed_roots=None):
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
    root_resolved = root.resolve(strict=True)
    allowed = _allowed_bases(root_resolved, allowed_roots)
    if root.is_symlink() and (
        allowed_roots is None
        or not any(root_resolved == base or root_resolved.is_relative_to(base) for base in allowed)
    ):
        raise PreflightError("local_engine_directory_symlink")
    root_stat = root.stat()
    result = {}
    pending = [(str(root), "", frozenset({(root_stat.st_dev, root_stat.st_ino)}))]
    while pending:
        directory, prefix, ancestors = pending.pop()
        with os.scandir(directory) as entries:
            for entry in entries:
                info = entry.stat(follow_symlinks=False)
                name = prefix + entry.name
                if entry.is_symlink():
                    try:
                        target_stat = entry.stat()
                        target = Path(entry.path).resolve(strict=True)
                    except (OSError, RuntimeError) as exc:
                        raise PreflightError("local_engine_directory_symlink") from exc
                    if stat.S_ISDIR(target_stat.st_mode):
                        dir_id = (target_stat.st_dev, target_stat.st_ino)
                        if dir_id in ancestors or not any(
                            target == base or target.is_relative_to(base) for base in allowed
                        ):
                            raise PreflightError("local_engine_directory_symlink")
                        pending.append((entry.path, name + "/", ancestors | {dir_id}))
                    else:
                        result[name] = stamp(entry.path, info, target_stat)
                elif entry.is_dir(follow_symlinks=False):
                    dir_id = (info.st_dev, info.st_ino)
                    pending.append((entry.path, name + "/", ancestors | {dir_id}))
                else:
                    result[name] = stamp(entry.path, info, entry.stat())
    return result


def _python(description):
    executable = Path(description["executable"])
    if not (executable.parent.parent / "pyvenv.cfg").is_file():
        raise PreflightError("local_engine_python_venv_required")
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
    paths += [Path(declaration["executable"]).resolve(strict=True)]
    if declaration["family"] in DIRECTORY_FAMILIES:
        from importlib.resources import files

        paths.append(Path(str(files("inferyard").joinpath("data/local_engine/bootstrap.py"))))
    if declaration["family"] == "mlx-lm":
        paths += [
            Path(str(files("inferyard").joinpath("data/local_engine/mlx_server.py"))),
            Path(str(files("inferyard").joinpath("data/engine_fit/mlx_server.py"))),
        ]
    if declaration["family"] == "polystrata":
        from inferyard.platforms.local_engines.detect_polystrata import (
            POLYSTRATA_BUILD_JSON_NAMES,
            resolve_polystrata_server_script,
            venv_base_python,
        )

        root = Path(declaration["working_directory"])
        server_py = resolve_polystrata_server_script(root)
        if server_py is not None:
            paths.append(server_py)
        for tok_rel in ("terminal/tools/strata_tokenizer.py", "tools/strata_tokenizer.py"):
            tokenizer_tool = root / tok_rel
            if tokenizer_tool.is_file():
                paths.append(tokenizer_tool)
        for name in _POLYSTRATA_TOP_FILES:
            candidate = root / name
            if candidate.is_file():
                paths.append(candidate)
        for engine_dir in _POLYSTRATA_ENGINE_DIRS:
            for build_name in POLYSTRATA_BUILD_JSON_NAMES:
                build_json = root / engine_dir / build_name
                if build_json.is_file():
                    paths.append(build_json)
        exe_path = Path(declaration["executable"])
        pyvenv_cfg = exe_path.parent.parent / "pyvenv.cfg"
        if pyvenv_cfg.is_file():
            paths.append(pyvenv_cfg)
        if platform.system() == "Windows":
            base_py = venv_base_python(exe_path)
            if base_py is not None and base_py.is_file():
                paths.append(base_py)
    selected = []
    for path in sorted(set(paths), key=lambda p: (len(p.parts), str(p))):
        if not any(parent.is_dir() and path.is_relative_to(parent) for parent in selected):
            selected.append(path)
    return selected


def _record_disk_cache(disk_entries: dict, path: Path, identity: FileIdentity) -> bool:
    try:
        st = path.stat()
        if (st.st_dev, st.st_ino, st.st_size, st.st_mtime_ns) != (
            identity.device,
            identity.inode,
            identity.size,
            identity.mtime_ns,
        ):
            return False
        disk_entries[str(path)] = [
            st.st_dev,
            st.st_ino,
            st.st_size,
            st.st_mtime_ns,
            st.st_ctime_ns,
            identity.path,
            identity.sha256,
        ]
        return True
    except OSError:
        return False


def _snapshot(roots, *, allowed_roots=None):
    snapshots, identities = [], []
    disk_entries = _load_disk_cache()
    disk_dirty = False
    for root in roots:
        before = inventory(root, allowed_roots=allowed_roots)
        directory = root.is_dir()
        entries = []
        for name, stamps in sorted(before.items()):
            path = root / name if directory else root
            cache_key = (str(path), stamps)
            identity = _FILE_CACHE.get(cache_key)
            if identity is None:
                identity = _lookup_disk_cache(disk_entries, path, stamps)
                if identity is None:
                    identity = hash_file(path)
                    if _record_disk_cache(disk_entries, path, identity):
                        disk_dirty = True
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
        if inventory(root, allowed_roots=allowed_roots) != before:
            raise PreflightError("local_engine_runtime_changed_during_hash")
        snapshots.append({"path": str(root), "entries": entries})
    if disk_dirty:
        _save_disk_cache(disk_entries)
    return snapshots, identities


def _freeze(value):
    family = value.get("family") if isinstance(value, dict) else None
    check_family_platform(family)
    try:
        declaration = description(value)
        if declaration["family"] == "polystrata":
            _verify_polystrata_layout(declaration)
        python, paths = (None, [])
        if declaration["family"] in DIRECTORY_FAMILIES:
            python, paths = _python(declaration)
        roots = _roots(declaration, paths)
        if python is not None:
            from inferyard.platforms.local_engines.python_sources import resolve_sources

            resolve_sources(python, declaration, roots)
        allowed_roots = [Path(p) for p in declaration["runtime_roots"]]
        allowed_roots.extend(p for p in roots if p.is_dir())
        if python is not None:
            allowed_roots.extend([Path(python["base_prefix"]), Path(python["prefix"])])
        snapshots, identities = _snapshot(roots, allowed_roots=allowed_roots)
        if platform.system() == "Darwin":
            from inferyard.platforms.local_engines.closure import verify_libraries

            unresolved = verify_libraries(
                [Path(item.path) for item in identities], Path(declaration["executable"])
            )
        else:
            unresolved = []
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
    family = (
        snapshot.get("installation", {}).get("family")
        if isinstance(snapshot, dict) and isinstance(snapshot.get("installation"), dict)
        else None
    )
    check_family_platform(family)
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

    validate_value(snapshot, schema, "local_engine_runtime")
    current, identities = _freeze(snapshot["installation"])
    if current != snapshot:
        raise PreflightError("local_engine_runtime_changed")
    return identities
