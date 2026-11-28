"""Cache and stage multi-engine assets into an isolated workspace for real UI E2E."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import urllib.request
from dataclasses import dataclass
from pathlib import Path

from polystrata_stage import (
    StagedPolystrata,
    clone_tree_cow,
    resolve_local_installation,
    snapshot_dir_state,
    stage_local_polystrata,
    verify_dir_unchanged,
)

ALL_UPGRADABLE_ENGINES: tuple[str, ...] = ("llama-cpp", "mlx-lm", "vllm", "polystrata")
LIGHTWEIGHT_ENGINES: tuple[str, ...] = ("llama-cpp", "mlx-lm", "vllm")

LLAMA_BASE_URL = (
    "https://github.com/PrismML-Eng/llama.cpp/releases/download/"
    "prism-b10743-adfffbe/llama-prism-b10743-adfffbe-bin-macos-arm64.tar.gz"
)
LLAMA_BASE_SHA256 = "596d257973080ca5011a4be50477c5f93ed1d231fcccfc2afb44bb573bb9629a"
GGUF_MODEL_REPO = "unsloth/SmolLM2-135M-Instruct-GGUF"
GGUF_MODEL_REV = "9e6855bc4be717fca1ef21360a1db4b29d5c559a"
GGUF_MODEL_FILE = "SmolLM2-135M-Instruct-Q4_K_M.gguf"
GGUF_MODEL_URL = (
    f"https://huggingface.co/{GGUF_MODEL_REPO}/blob/"
    f"{GGUF_MODEL_REV}/{GGUF_MODEL_FILE}"
)

MLX_BASE_VERSION = "0.31.1"
MLX_MODEL_REPO = "mlx-community/SmolLM2-135M-Instruct-8bit"
MLX_MODEL_REV = "0f0d9b8218915bc34d401e1a340b8c049d300d5e"
MLX_MODEL_URL = f"https://huggingface.co/{MLX_MODEL_REPO}/tree/{MLX_MODEL_REV}"
MLX_MODEL_NAME = "smollm2-mlx"

VLLM_BASE_VERSION = "0.31.0"
VLLM_BASE_WHEELS: tuple[str, ...] = (
    "https://github.com/vllm-project/vllm/releases/download/v0.31.0/"
    "vllm-0.31.0%2Bcpu-cp312-cp312-macosx_11_0_arm64.whl",
    "https://github.com/vllm-project/vllm-metal/releases/download/v0.31.0/"
    "vllm_metal-0.31.0-cp312-cp312-macosx_15_0_arm64.whl",
)
HF_MODEL_REPO = "HuggingFaceTB/SmolLM2-135M-Instruct"
HF_MODEL_REV = "12fd25f77366fa6b3b4b768ec3050bf629380bac"
HF_MODEL_NAME = "smollm2-hf"
HF_MODEL_FILES: tuple[str, ...] = (
    "config.json",
    "generation_config.json",
    "model.safetensors",
    "special_tokens_map.json",
    "tokenizer.json",
    "tokenizer_config.json",
)

POLYSTRATA_DOWNGRADED_VERSION = "v0.2.20-poly.4"


@dataclass(frozen=True)
class StagedEnginesEnv:
    home: Path
    workspace: Path
    python_path: Path
    engines: tuple[str, ...]
    polystrata: StagedPolystrata | None
    host_inferyard: Path | None
    host_inferyard_snapshot: dict[str, str] | None


def parse_engines_arg(raw: str, *, lightweight: bool = False) -> tuple[str, ...]:
    if lightweight:
        return LIGHTWEIGHT_ENGINES
    cleaned = raw.strip().lower()
    if not cleaned or cleaned == "all":
        return ALL_UPGRADABLE_ENGINES
    if cleaned == "lightweight":
        return LIGHTWEIGHT_ENGINES
    picked: list[str] = []
    for part in cleaned.split(","):
        token = part.strip()
        if not token:
            continue
        if token not in ALL_UPGRADABLE_ENGINES:
            allowed = ", ".join((*ALL_UPGRADABLE_ENGINES, "all", "lightweight"))
            raise ValueError(f"unsupported engine {token!r}; expected one of: {allowed}")
        if token not in picked:
            picked.append(token)
    if not picked:
        raise ValueError("at least one engine must be selected")
    return tuple(picked)


def _find_uv() -> str:
    found = shutil.which("uv")
    if found:
        return found
    raise FileNotFoundError("uv executable is required on PATH to stage Python engine venvs")


def _download_file(url: str, dst: Path) -> None:
    dst.parent.mkdir(parents=True, exist_ok=True)
    tmp = dst.with_name(f".{dst.name}.tmp")
    req = urllib.request.Request(url, headers={"User-Agent": "inferyard-ui-e2e/1.0"})
    with urllib.request.urlopen(req, timeout=180) as resp, tmp.open("wb") as out:
        shutil.copyfileobj(resp, out)
    tmp.replace(dst)


def ensure_cached_llama_assets(cache_dir: Path, cli: Path, cli_env: dict[str, str]) -> tuple[Path, Path]:
    eng_dir = cache_dir / "engines/llama-prism-b10743"
    if not any(eng_dir.rglob("llama-server")):
        shutil.rmtree(eng_dir, ignore_errors=True)
        eng_dir.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run(
            [
                str(cli),
                "engine",
                "prepare",
                "--source",
                LLAMA_BASE_URL,
                "--expected-sha256",
                LLAMA_BASE_SHA256,
                "--out",
                str(eng_dir),
            ],
            env=cli_env,
            check=True,
        )
    model_dir = cache_dir / "models/smollm2-gguf"
    gguf_file = model_dir / GGUF_MODEL_FILE
    if not gguf_file.is_file():
        shutil.rmtree(model_dir, ignore_errors=True)
        model_dir.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run(
            [
                str(cli),
                "model",
                "acquire",
                "--kind",
                "gguf",
                "--source",
                GGUF_MODEL_URL,
                "--out",
                str(model_dir),
            ],
            env=cli_env,
            check=True,
        )
    return eng_dir, model_dir


def ensure_cached_mlx_assets(cache_dir: Path, cli: Path, cli_env: dict[str, str]) -> tuple[Path, Path]:
    venv_dir = cache_dir / "engines/mlx-lm-base"
    py_bin = venv_dir / "bin/python3"
    has_mlx = any(venv_dir.glob("lib/python*/site-packages/mlx_lm-*.dist-info/METADATA"))
    if not py_bin.is_file() or not has_mlx:
        shutil.rmtree(venv_dir, ignore_errors=True)
        venv_dir.parent.mkdir(parents=True, exist_ok=True)
        uv = _find_uv()
        subprocess.run([uv, "venv", "--python", "3.12", str(venv_dir)], check=True)
        subprocess.run(
            [uv, "pip", "install", "--python", str(py_bin), f"mlx-lm=={MLX_BASE_VERSION}"],
            check=True,
        )
    model_dir = cache_dir / f"models/{MLX_MODEL_NAME}"
    if not (model_dir / "config.json").is_file():
        shutil.rmtree(model_dir, ignore_errors=True)
        model_dir.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run(
            [
                str(cli),
                "model",
                "acquire",
                "--kind",
                "directory",
                "--source",
                MLX_MODEL_URL,
                "--out",
                str(model_dir),
            ],
            env=cli_env,
            check=True,
        )
    return venv_dir, model_dir


def ensure_cached_vllm_assets(cache_dir: Path) -> tuple[Path, Path]:
    venv_dir = cache_dir / "engines/vllm-base"
    py_bin = venv_dir / "bin/python3"
    has_metal = any(venv_dir.glob("lib/python*/site-packages/vllm_metal-*.dist-info/METADATA"))
    if not py_bin.is_file() or not has_metal:
        shutil.rmtree(venv_dir, ignore_errors=True)
        venv_dir.parent.mkdir(parents=True, exist_ok=True)
        uv = _find_uv()
        subprocess.run([uv, "venv", "--python", "3.12", str(venv_dir)], check=True)
        subprocess.run(
            [uv, "pip", "install", "--python", str(py_bin), *VLLM_BASE_WHEELS],
            check=True,
        )
    model_dir = cache_dir / f"models/{HF_MODEL_NAME}"
    if not (model_dir / "model.safetensors").is_file():
        shutil.rmtree(model_dir, ignore_errors=True)
        model_dir.mkdir(parents=True, exist_ok=True)
        base = f"https://huggingface.co/{HF_MODEL_REPO}/resolve/{HF_MODEL_REV}"
        for filename in HF_MODEL_FILES:
            _download_file(f"{base}/{filename}", model_dir / filename)
    return venv_dir, model_dir


def _init_managed_venv_engine(
    cli: Path,
    cli_env: dict[str, str],
    src_venv: Path,
    dst_dir: Path,
    *,
    family: str,
    version: str,
    release_id: str,
    build_id: str,
    source_url: str,
) -> Path:
    dst_venv = dst_dir / "venv"
    clone_tree_cow(src_venv, dst_venv)
    py_bin = dst_venv / "bin/python3"
    inst_path = dst_dir / "installation.json"
    subprocess.run(
        [
            str(cli),
            "engine",
            "init-installation",
            "--family",
            family,
            "--executable",
            str(py_bin),
            "--working-directory",
            str(dst_dir),
            "--runtime-root",
            str(dst_venv),
            "--out",
            str(inst_path),
        ],
        env=cli_env,
        check=True,
    )
    receipt = {
        "definition": "inferyard-managed-engine.v1",
        "family": family,
        "version": version,
        "release_id": release_id,
        "build_id": build_id,
        "install_mode": "managed_venv",
        "source_url": source_url,
        "installed_at": "2026-01-01T00:00:00Z",
    }
    receipt_path = dst_dir / ".inferyard-managed-engine.json"
    receipt_path.write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
    receipt_path.chmod(0o600)
    return inst_path


def downgrade_staged_polystrata_version(work_dir: Path) -> None:
    (work_dir / ".inferyard-runtime.json").unlink(missing_ok=True)
    wrote_any = False
    for sub in (
        "terminal/engine-poly",
        "terminal/engine",
        "engine/build-metal",
        "engine-poly",
        "engine",
        "build-metal",
        "",
    ):
        base = work_dir / sub if sub else work_dir
        for name in ("BUILD.json", "BUILD-poly.json", "BUILD-q36.json"):
            cand = base / name
            if cand.is_file() and not cand.is_symlink():
                try:
                    doc = json.loads(cand.read_text(encoding="utf-8"))
                except (OSError, json.JSONDecodeError):
                    doc = {}
                if not isinstance(doc, dict):
                    doc = {}
                doc["version"] = POLYSTRATA_DOWNGRADED_VERSION
                doc.setdefault("backend", "metal")
                cand.write_text(json.dumps(doc, indent=2) + "\n", encoding="utf-8")
                wrote_any = True
    if not wrote_any:
        fallback = work_dir / "engine/build-metal/BUILD-poly.json"
        fallback.parent.mkdir(parents=True, exist_ok=True)
        fallback.write_text(
            json.dumps({"version": POLYSTRATA_DOWNGRADED_VERSION, "backend": "metal"}, indent=2)
            + "\n",
            encoding="utf-8",
        )


def stage_engines_environment(
    output: Path,
    cli: Path,
    cache_dir: Path,
    engines: tuple[str, ...],
    *,
    polystrata_installation: Path | None = None,
    host_home: Path | None = None,
) -> StagedEnginesEnv:
    user_home = host_home if host_home is not None else Path.home()
    host_inferyard = user_home / ".inferyard"
    host_snap = snapshot_dir_state(host_inferyard) if host_inferyard.is_dir() else None

    host_dir = output / "host"
    host_dir.mkdir(parents=True, exist_ok=True)
    bootstrap = Path(__file__).resolve().parents[1] / "e2e/cli_bootstrap.py"
    shutil.copyfile(bootstrap, host_dir / "sitecustomize.py")
    existing_py = os.environ.get("PYTHONPATH", "").strip()
    python_path = f"{host_dir}{os.pathsep}{existing_py}" if existing_py else str(host_dir)

    workspace = output / "workspace"
    cli_env = dict(os.environ, PYTHONPATH=python_path)
    subprocess.run([str(cli), "init", "--out", str(workspace)], env=cli_env, check=True)

    installations: list[str] = []
    if "llama-cpp" in engines:
        llama_src, gguf_src = ensure_cached_llama_assets(cache_dir, cli, cli_env)
        clone_tree_cow(llama_src, workspace / "engines/llama-prism-b10743")
        clone_tree_cow(gguf_src, workspace / "models/smollm2-gguf")

    if "mlx-lm" in engines:
        mlx_venv, mlx_model = ensure_cached_mlx_assets(cache_dir, cli, cli_env)
        mlx_inst = _init_managed_venv_engine(
            cli,
            cli_env,
            mlx_venv,
            workspace / "engines/mlx-lm-managed",
            family="mlx-lm",
            version=MLX_BASE_VERSION,
            release_id="mlx-lm-0.31.1",
            build_id="mlx-lm-darwin-arm64",
            source_url="https://pypi.org/project/mlx-lm/",
        )
        installations.append(str(mlx_inst))
        clone_tree_cow(mlx_model, workspace / f"models/{MLX_MODEL_NAME}")

    if "vllm" in engines:
        vllm_venv, hf_model = ensure_cached_vllm_assets(cache_dir)
        vllm_inst = _init_managed_venv_engine(
            cli,
            cli_env,
            vllm_venv,
            workspace / "engines/vllm-managed",
            family="vllm",
            version=f"v{VLLM_BASE_VERSION}",
            release_id="vllm-metal-v0.31.0",
            build_id="vllm-darwin-arm64",
            source_url=VLLM_BASE_WHEELS[0],
        )
        installations.append(str(vllm_inst))
        clone_tree_cow(hf_model, workspace / f"models/{HF_MODEL_NAME}")

    staged_ps: StagedPolystrata | None = None
    if "polystrata" in engines:
        ps_src = resolve_local_installation(polystrata_installation, home=user_home)
        staged_ps = stage_local_polystrata(ps_src, workspace)
        downgrade_staged_polystrata_version(staged_ps.work_dir)
        installations.append(str(staged_ps.installation_path))

    home = output / "home"
    settings_dir = home / "Library/Application Support/InferYard Studio"
    settings_dir.mkdir(parents=True, exist_ok=True)
    settings_dir.chmod(0o700)
    settings = {
        "inferyard_path": str(cli),
        "workspace_root": str(workspace),
        "keep_services_on_quit": False,
        "engine_installations": installations,
    }
    settings_path = settings_dir / "settings.json"
    settings_path.write_text(json.dumps(settings, indent=2) + "\n", encoding="utf-8")
    settings_path.chmod(0o600)
    return StagedEnginesEnv(
        home=home,
        workspace=workspace,
        python_path=Path(python_path),
        engines=engines,
        polystrata=staged_ps,
        host_inferyard=host_inferyard if host_snap is not None else None,
        host_inferyard_snapshot=host_snap,
    )


def verify_host_assets_unchanged(staged: StagedEnginesEnv) -> None:
    if staged.polystrata is not None:
        verify_dir_unchanged(
            "host PolyStrata working_directory",
            staged.polystrata.src_work_dir,
            staged.polystrata.work_snapshot,
        )
        verify_dir_unchanged(
            "host PolyStrata data_dir",
            staged.polystrata.src_data_dir,
            staged.polystrata.data_snapshot,
        )
    if staged.host_inferyard is not None and staged.host_inferyard_snapshot is not None:
        verify_dir_unchanged(
            "host ~/.inferyard",
            staged.host_inferyard,
            staged.host_inferyard_snapshot,
        )
