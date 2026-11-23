"""Shared Python and venv creator resolution for managed PolyStrata setup."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
from typing import Literal

POLYSTRATA_MIN_PYTHON = (3, 10)
POLYSTRATA_MAX_PYTHON = (3, 13)
POLYSTRATA_PREFERRED_VERSIONS = ((3, 12), (3, 11), (3, 13), (3, 10))
POLYSTRATA_UV_PYTHON = "3.12"
_PYTHON_CANDIDATE_NAMES = (
    "python3.12",
    "python3.11",
    "python3.13",
    "python3.10",
    "python3",
    "python",
)
_VERSION_RE = re.compile(r"^\s*(\d+)\.(\d+)\s*$")


@dataclass(frozen=True, slots=True)
class VenvCreator:
    """Resolved creator (`uv` or Python 3.10-3.13 executable) for PolyStrata's `.venv`."""

    kind: Literal["uv", "python"]
    executable: str
    python_version: tuple[int, int] | None = None
    explicit: bool = False

    def to_dict(self) -> dict:
        return {
            "kind": self.kind,
            "executable": self.executable,
            "python_version": list(self.python_version) if self.python_version else None,
            "explicit": self.explicit,
        }

    @classmethod
    def from_dict(cls, data: dict) -> VenvCreator:
        kind_raw = data.get("kind")
        kind: Literal["uv", "python"] = "uv" if kind_raw == "uv" else "python"
        ver_raw = data.get("python_version")
        version: tuple[int, int] | None = None
        if (
            isinstance(ver_raw, (list, tuple))
            and len(ver_raw) == 2
            and all(type(item) is int for item in ver_raw)
        ):
            version = (ver_raw[0], ver_raw[1])
        return cls(
            kind=kind,
            executable=str(data.get("executable") or ""),
            python_version=version,
            explicit=bool(data.get("explicit", False)),
        )


def _lookup_env(env: dict[str, str], key: str, *, system: str) -> tuple[bool, str | None]:
    if key in env:
        return True, env[key]
    if system == "Windows":
        target = key.lower()
        for existing_key, value in env.items():
            if existing_key.lower() == target:
                return True, value
    return False, None


def _search_path(env: dict[str, str] | None, *, system: str) -> str:
    if env is not None:
        found, value = _lookup_env(env, "PATH", system=system)
        if found:
            return value or ""
    return os.environ.get("PATH", "")


def _is_executable_file(path: Path, *, system: str) -> bool:
    try:
        if not path.is_file():
            return False
        if system == "Windows":
            return True
        return os.access(path, os.X_OK)
    except OSError:
        return False


def _resolve_explicit_python(raw: str, *, search_path: str, system: str) -> str | None:
    if any(ch in raw for ch in ("\x00", "\r", "\n")):
        return None
    text = raw.strip()
    if not text:
        return None
    looks_like_path = (
        os.sep in text
        or "/" in text
        or (system == "Windows" and "\\" in text)
        or text.startswith("~")
        or Path(text).is_absolute()
    )
    if looks_like_path:
        candidate = Path(text).expanduser()
        if not _is_executable_file(candidate, system=system):
            return None
        return str(candidate.absolute())
    if not search_path:
        return None
    found = shutil.which(text, path=search_path)
    return str(Path(found).absolute()) if found else None


def probe_python_interpreter(
    executable: str, *, system: str | None = None
) -> tuple[tuple[int, int], str] | None:
    """Return `((major, minor), concrete_executable)` for a Python executable."""
    target_system = system or platform.system()
    try:
        res = subprocess.run(
            [
                executable,
                "-c",
                "import sys; print(f'{sys.version_info[0]}.{sys.version_info[1]}\\n{sys.executable}')",
            ],
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if res.returncode != 0:
        return None
    lines = [line.strip() for line in res.stdout.splitlines() if line.strip()]
    if not lines:
        return None
    match = _VERSION_RE.match(lines[0])
    if match is None:
        return None
    version = (int(match.group(1)), int(match.group(2)))
    resolved_exe = str(Path(executable).absolute())
    if len(lines) >= 2:
        reported = Path(lines[1])
        if _is_executable_file(reported, system=target_system):
            resolved_exe = str(reported.absolute())
    return version, resolved_exe


def probe_python_version(executable: str) -> tuple[int, int] | None:
    """Return `(major, minor)` for a Python executable, or None if unavailable."""
    probed = probe_python_interpreter(executable, system=platform.system())
    return probed[0] if probed is not None else None


def check_python_venv(python_executable: str) -> bool:
    """Return True if the Python executable supports `python -m venv` with `ensurepip`."""
    try:
        help_res = subprocess.run(
            [python_executable, "-m", "venv", "--help"],
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )
        if help_res.returncode != 0:
            return False
        ensure_res = subprocess.run(
            [python_executable, "-c", "import venv, ensurepip"],
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )
        return ensure_res.returncode == 0
    except (OSError, subprocess.SubprocessError):
        return False


def _find_uv(env: dict[str, str] | None, *, search_path: str, system: str) -> str | None:
    candidates: list[str] = []
    if search_path:
        found = shutil.which("uv", path=search_path)
        if found:
            candidates.append(found)
    source_env = os.environ if env is None else env
    has_tools, tools_dir = _lookup_env(dict(source_env), "INFERYARD_TOOLS_DIR", system=system)
    if has_tools and tools_dir and tools_dir.strip():
        tools_raw = tools_dir.strip()
        found_tools = shutil.which("uv", path=tools_raw)
        if found_tools:
            candidates.append(found_tools)
        uv_name = "uv.exe" if system == "Windows" else "uv"
        candidates.append(str(Path(tools_raw) / uv_name))

    seen: set[str] = set()
    for raw_cand in candidates:
        cand_path = Path(raw_cand).absolute()
        key = os.path.normcase(str(cand_path))
        if key in seen or not _is_executable_file(cand_path, system=system):
            continue
        seen.add(key)
        try:
            res = subprocess.run(
                [str(cand_path), "--version"],
                stdin=subprocess.DEVNULL,
                capture_output=True,
                text=True,
                timeout=10,
                check=False,
            )
            if res.returncode == 0:
                return str(cand_path)
        except (OSError, subprocess.SubprocessError):
            continue
    return None


def _discover_local_pythons(*, search_path: str, system: str) -> list[str]:
    if not search_path:
        return []
    raw_candidates: list[str] = []
    for name in _PYTHON_CANDIDATE_NAMES:
        found = shutil.which(name, path=search_path)
        if found:
            raw_candidates.append(found)
    if system == "Windows":
        py_launcher = shutil.which("py", path=search_path)
        if py_launcher:
            for maj, min_ver in POLYSTRATA_PREFERRED_VERSIONS:
                try:
                    res = subprocess.run(
                        [py_launcher, f"-{maj}.{min_ver}", "-c", "import sys; print(sys.executable)"],
                        stdin=subprocess.DEVNULL,
                        capture_output=True,
                        text=True,
                        timeout=5,
                        check=False,
                    )
                except (OSError, subprocess.SubprocessError):
                    continue
                exe = res.stdout.strip()
                if res.returncode == 0 and exe:
                    raw_candidates.append(exe)

    unique: list[str] = []
    seen_paths: set[str] = set()
    for path in raw_candidates:
        abs_path = os.path.abspath(path)
        key = os.path.normcase(abs_path)
        if key in seen_paths:
            continue
        seen_paths.add(key)
        unique.append(abs_path)
    return unique


def _install_python_fix_command(system: str) -> str:
    if system == "Darwin":
        return "brew install python@3.12"
    if system == "Linux":
        return "sudo apt install python3.12 python3.12-venv"
    if system == "Windows":
        return "winget install --id Python.Python.3.12"
    return "uv venv --seed --python 3.12"


def _venv_missing_fix_command(system: str, version: tuple[int, int] | None = None) -> str:
    if system == "Linux":
        if version is not None:
            return f"sudo apt install python{version[0]}.{version[1]}-venv"
        return "sudo apt install python3-venv"
    if system == "Darwin":
        return "brew install python@3.12"
    if system == "Windows":
        return "winget install --id Python.Python.3.12"
    return "sudo apt install python3-venv"


def is_supported_python_version(version: tuple[int, int] | None) -> bool:
    return (
        version is not None
        and POLYSTRATA_MIN_PYTHON <= version <= POLYSTRATA_MAX_PYTHON
    )


def resolve_polystrata_venv_creator(
    *,
    system: str,
    venv_python: str | None = None,
    env: dict[str, str] | None = None,
    venv_check: Callable[[str], bool] | None = None,
    version_probe: Callable[[str], tuple[int, int] | None] | None = None,
) -> tuple[VenvCreator | None, list[dict]]:
    """Resolve explicit `--python`, `uv`, or Python 3.10-3.13; never silently accept 3.15."""
    search_path = _search_path(env, system=system)
    check_venv = venv_check if venv_check is not None else check_python_venv

    def _probe(executable: str) -> tuple[tuple[int, int] | None, str]:
        if version_probe is not None:
            return version_probe(executable), str(Path(executable).absolute())
        probed = probe_python_interpreter(executable, system=system)
        if probed is None:
            return None, str(Path(executable).absolute())
        return probed[0], probed[1]

    if venv_python is not None:
        resolved = _resolve_explicit_python(venv_python, search_path=search_path, system=system)
        if resolved is None:
            return None, [
                {
                    "code": "polystrata_python_unsupported",
                    "message": (
                        f"Explicit --python '{venv_python}' was not found or is not executable; "
                        "PolyStrata requires Python 3.10-3.13."
                    ),
                    "fix_command": _install_python_fix_command(system),
                }
            ]
        version, concrete_exe = _probe(resolved)
        if not is_supported_python_version(version):
            ver_label = f"{version[0]}.{version[1]}" if version is not None else "unknown"
            return None, [
                {
                    "code": "polystrata_python_unsupported",
                    "message": (
                        f"Explicit --python '{resolved}' is Python {ver_label}; "
                        "PolyStrata requires Python 3.10-3.13."
                    ),
                    "fix_command": _install_python_fix_command(system),
                }
            ]
        assert version is not None
        if not check_venv(concrete_exe):
            return None, [
                {
                    "code": "polystrata_python_venv_missing",
                    "message": (
                        f"Python {version[0]}.{version[1]} at '{concrete_exe}' lacks venv/ensurepip support."
                    ),
                    "fix_command": _venv_missing_fix_command(system, version=version),
                }
            ]
        return (
            VenvCreator(
                kind="python", executable=concrete_exe, python_version=version, explicit=True
            ),
            [],
        )

    uv_path = _find_uv(env, search_path=search_path, system=system)
    if uv_path is not None:
        return VenvCreator(kind="uv", executable=uv_path, python_version=(3, 12), explicit=False), []

    candidates = _discover_local_pythons(search_path=search_path, system=system)
    compatible: list[tuple[int, int, str, tuple[int, int]]] = []
    incompatible_versions: list[str] = []
    for order_idx, cand in enumerate(candidates):
        ver, concrete_exe = _probe(cand)
        if is_supported_python_version(ver):
            assert ver is not None
            priority = POLYSTRATA_PREFERRED_VERSIONS.index(ver)
            compatible.append((priority, order_idx, concrete_exe, ver))
        elif ver is not None:
            ver_text = f"{ver[0]}.{ver[1]}"
            if ver_text not in incompatible_versions:
                incompatible_versions.append(ver_text)

    compatible.sort(key=lambda item: (item[0], item[1]))
    deduped_compatible: list[tuple[int, int, str, tuple[int, int]]] = []
    seen_concrete: set[str] = set()
    for item in compatible:
        key = os.path.normcase(item[2])
        if key in seen_concrete:
            continue
        seen_concrete.add(key)
        deduped_compatible.append(item)

    for _, _, concrete_exe, ver in deduped_compatible:
        if check_venv(concrete_exe):
            return (
                VenvCreator(
                    kind="python", executable=concrete_exe, python_version=ver, explicit=False
                ),
                [],
            )

    if deduped_compatible:
        return None, [
            {
                "code": "polystrata_python_venv_missing",
                "message": "Python venv support (with ensurepip) is required for PolyStrata.",
                "fix_command": _venv_missing_fix_command(system, version=deduped_compatible[0][3]),
            }
        ]

    detail = (
        f" (found unsupported Python {', '.join(incompatible_versions)})"
        if incompatible_versions
        else ""
    )
    return None, [
        {
            "code": "polystrata_python_unsupported",
            "message": (
                f"No compatible Python 3.10-3.13 or uv was found{detail}; "
                "PolyStrata does not use Python 3.15."
            ),
            "fix_command": _install_python_fix_command(system),
        }
    ]


def build_venv_command(creator: VenvCreator, venv_dir: Path) -> list[str]:
    """Build the subprocess command used to create PolyStrata's `.venv`."""
    if creator.kind == "uv":
        return [
            creator.executable,
            "venv",
            "--no-project",
            "--seed",
            "--python",
            POLYSTRATA_UV_PYTHON,
            str(venv_dir),
        ]
    return [creator.executable, "-m", "venv", str(venv_dir)]
