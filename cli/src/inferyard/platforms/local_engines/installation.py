"""Lightweight local engine installation validation and executable identity."""

import os
import platform
from pathlib import Path

from inferyard.contracts.local_environment import validate_installation_environment
from inferyard.contracts.schemas_local_engines import INSTALLATION
from inferyard.contracts.validation import strict_json_loads, validate_value
from inferyard.errors import PreflightError
from inferyard.platforms.identity import hash_file

RUNTIME = "local-engine-runtime.v1"


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
    raw_roots = result.get("runtime_roots")
    if not raw_roots:
        raw_roots = [result["working_directory"]]
    roots = [Path(item) for item in raw_roots]
    if any(not p.is_absolute() or not p.is_dir() for p in roots):
        raise PreflightError("local_engine_runtime_root_invalid")
    result["runtime_roots"] = sorted({str(p.resolve(strict=True)) for p in roots})
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


def _freeze(value):
    family = value.get("family") if isinstance(value, dict) else None
    check_family_platform(family)
    try:
        declaration = description(value)
        if declaration["family"] == "polystrata":
            _verify_polystrata_layout(declaration)
        exe_resolved = Path(declaration["executable"]).resolve(strict=True)
        identity = hash_file(exe_resolved)
        roots = [
            {
                "path": str(exe_resolved),
                "entries": [
                    {
                        "path": exe_resolved.name,
                        "sha256": identity.sha256,
                        "size": identity.size,
                        "symlink": None,
                    }
                ],
            }
        ]
        return {
            "schema_version": 3,
            "definition": RUNTIME,
            "installation": declaration,
            "python": None,
            "roots": roots,
            "unresolved_libraries": [],
        }, [identity]
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
    _, identities = _freeze(snapshot["installation"])
    return identities
