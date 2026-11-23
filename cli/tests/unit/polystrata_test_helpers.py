"""Shared test helpers and fake binaries for PolyStrata unit tests."""

from __future__ import annotations

from contextlib import contextmanager
import gzip
import io
import os
from pathlib import Path
import sys
import tarfile
from types import SimpleNamespace
import zipfile

import httpx


def _disk_usage(free_bytes: int):
    return lambda _path: SimpleNamespace(total=free_bytes * 2, used=free_bytes, free=free_bytes)


def _tar_gz_file(path: Path, entries: list[tuple]) -> Path:
    buf = io.BytesIO()
    with gzip.GzipFile(fileobj=buf, mode="wb", mtime=0) as gz:
        with tarfile.open(fileobj=gz, mode="w") as package:
            for entry in entries:
                kind, name, *rest = entry
                info = tarfile.TarInfo(name)
                if kind == "dir":
                    info.type = tarfile.DIRTYPE
                    info.mode = rest[0] if rest else 0o755
                    package.addfile(info)
                elif kind == "symlink":
                    info.type = tarfile.SYMTYPE
                    info.linkname = rest[0]
                    package.addfile(info)
                else:
                    data = rest[0]
                    info.size = len(data)
                    info.mode = rest[1] if len(rest) > 1 else 0o644
                    package.addfile(info, io.BytesIO(data))
    path.write_bytes(buf.getvalue())
    return path


def _zip_file(path: Path, entries: dict[str, bytes]) -> Path:
    with zipfile.ZipFile(path, "w") as package:
        for name, data in entries.items():
            package.writestr(name, data)
    return path


def _make_fake_venv(out: Path) -> Path:
    """Fast lightweight venv wrapper pointing at current test Python (no ensurepip delay)."""
    venv = out / ".venv"
    bin_dir = venv / ("Scripts" if os.name == "nt" else "bin")
    bin_dir.mkdir(parents=True, exist_ok=True)
    (venv / "pyvenv.cfg").write_text("home = /usr/bin\ninclude-system-site-packages = false\n")
    py = bin_dir / ("python.exe" if os.name == "nt" else "python")
    if os.name == "nt":
        py.write_text(f'@echo off\n"{sys.executable}" %*\n', encoding="utf-8")
    else:
        py.write_text(f'#!/bin/sh\nexec "{sys.executable}" "$@"\n', encoding="utf-8")
        os.chmod(py, 0o755)
    return py


def _write_fake_python(
    bin_dir: Path,
    name: str,
    major: int,
    minor: int,
    *,
    venv_ok: bool = True,
) -> Path:
    bin_dir.mkdir(parents=True, exist_ok=True)
    venv_rc = 0 if venv_ok else 1
    if os.name == "nt":
        impl = bin_dir / f"_{name}_impl.py"
        impl.write_text(
            "import sys\n"
            "args = sys.argv[1:]\n"
            "if len(args) >= 2 and args[0] == '-c':\n"
            "    if 'ensurepip' in args[1]:\n"
            f"        sys.exit({venv_rc})\n"
            f"    print('{major}.{minor}')\n"
            "    sys.exit(0)\n"
            "if len(args) >= 3 and args[0] == '-m' and args[1] == 'venv' and args[2] == '--help':\n"
            f"    sys.exit({venv_rc})\n"
            "sys.exit(0)\n",
            encoding="utf-8",
        )
        cmd_path = bin_dir / f"{name}.cmd"
        cmd_path.write_text(
            f'@echo off\r\n"{sys.executable}" "{impl}" %*\r\n',
            encoding="utf-8",
        )
        return cmd_path

    path = bin_dir / name
    path.write_text(
        "#!/bin/sh\n"
        'if [ "$1" = "-c" ]; then\n'
        '  case "$2" in\n'
        '    *"ensurepip"*)\n'
        f"      exit {venv_rc}\n"
        "      ;;\n"
        "    *)\n"
        f'      echo "{major}.{minor}"\n'
        "      exit 0\n"
        "      ;;\n"
        "  esac\n"
        "fi\n"
        'if [ "$1" = "-m" ] && [ "$2" = "venv" ] && [ "$3" = "--help" ]; then\n'
        f"  exit {venv_rc}\n"
        "fi\n"
        "exit 0\n",
        encoding="utf-8",
    )
    os.chmod(path, 0o755)
    return path


def _write_fake_uv(bin_dir: Path, *, ok: bool = True) -> Path:
    bin_dir.mkdir(parents=True, exist_ok=True)
    rc = 0 if ok else 1
    if os.name == "nt":
        path = bin_dir / "uv.cmd"
        path.write_text(
            f"@echo off\r\nif \"%1\"==\"--version\" (\r\n  echo uv 0.13.6\r\n  exit /b {rc}\r\n)\r\nexit /b {rc}\r\n",
            encoding="utf-8",
        )
        return path
    path = bin_dir / "uv"
    path.write_text(
        "#!/bin/sh\n"
        'if [ "$1" = "--version" ]; then\n'
        '  echo "uv 0.13.6"\n'
        f"  exit {rc}\n"
        "fi\n"
        f"exit {rc}\n",
        encoding="utf-8",
    )
    os.chmod(path, 0o755)
    return path


class _FakeResponse:
    def __init__(self, body: bytes, *, status_code: int = 200, headers: dict | None = None):
        self._body = body
        self.status_code = status_code
        self.headers = httpx.Headers(headers or {"Content-Length": str(len(body))})

    def iter_raw(self):
        mid = len(self._body) // 2
        if mid:
            yield self._body[:mid]
        yield self._body[mid:]


class _FakeClient:
    def __init__(self, handler):
        self._handler = handler
        self.requests: list[tuple[str, str, dict]] = []

    @contextmanager
    def stream(self, method: str, url: str, *, headers: dict | None = None):
        req_headers = dict(headers or {})
        self.requests.append((method, url, req_headers))
        yield self._handler(method, url, req_headers)


_HAPPY_SETUP_PY = """\
import json, os, stat, struct, sys
from pathlib import Path

root = Path(__file__).resolve().parent
argv = sys.argv[1:]
(root / "received_argv.json").write_text(json.dumps(argv), encoding="utf-8")
(root / "received_env.json").write_text(
    json.dumps(
        {
            k: os.environ.get(k)
            for k in ("HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "no_proxy", "STRATA_SOURCE")
        }
    ),
    encoding="utf-8",
)

print("=== Step 3: Python packages ===", flush=True)
print("Authorization: Bearer secret_token_1234567890 hf_abcdefghijklmnop", flush=True)
print("=== Step 4: the PolyStrata engine ===", flush=True)
print("[ 50%] Building CXX object strata-poly.o", flush=True)
print("[100%] Linking CXX executable strata-poly", flush=True)
print("=== Step 5: downloading Qwen3.6-35B-A3B UD-Q4_K_M ===", flush=True)
sys.stdout.write("\\r  Qwen3.6.gguf: 1.50 / 3.00 GB (50%)   ")
sys.stdout.flush()
sys.stdout.write("\\r  Qwen3.6.gguf: 3.00 / 3.00 GB (100%)   \\n")
sys.stdout.flush()
print("=== Step 7: writing the start script ===", flush=True)

engine_dir = root / "engine-poly"
engine_dir.mkdir(exist_ok=True)
binary = engine_dir / ("strata-poly.exe" if os.name == "nt" else "strata-poly")
binary.write_text("#!/bin/sh\\necho strata-poly\\n", encoding="utf-8")
if os.name != "nt":
    os.chmod(binary, 0o755)
(engine_dir / "BUILD.json").write_text(
    json.dumps({"version": "v0.1.45-poly.10", "backend": "metal"}), encoding="utf-8"
)
data_dir = Path(argv[argv.index("--data-dir") + 1])
model_dir = data_dir / "models" / "qwen36-UD-Q4_K_M"
model_dir.mkdir(parents=True, exist_ok=True)
model_file = model_dir / "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf"
key_b = b"general.architecture"
val_b = b"qwen35moe"
gguf_bytes = (
    struct.pack("<4sIQQ", b"GGUF", 3, 1, 1)
    + struct.pack("<Q", len(key_b)) + key_b
    + struct.pack("<IQ", 8, len(val_b)) + val_b
    + b"tensors"
)
model_file.write_bytes(gguf_bytes)
(root / "strata-qwen36.json").write_text(
    json.dumps(
        {
            "exe": str(binary),
            "args": ["--model", str(model_file), "--max-context", "32768"],
            "cwd": str(root),
            "model_name": "qwen36-ud-q4-k-m",
            "port": 8080,
        }
    ),
    encoding="utf-8",
)
"""
