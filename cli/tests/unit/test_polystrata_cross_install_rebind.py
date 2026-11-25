"""Regression tests for cross-installation and migrated PolyStrata JSON rebinding."""

from __future__ import annotations

import json
import shutil
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.config.loader import load_config
from inferyard.config.local_engines.candidate import create as create_candidate
from inferyard.contracts.validation import json_bytes, validate_document
from inferyard.errors import ContractError
from inferyard.platforms.local_engines import installation, workers
from inferyard.platforms.local_engines.assets import assets as verify_assets
from inferyard.platforms.local_engines.detect import build_installation
from inferyard.provisioning.preparation_io import PreparationError


def _write_bundle(path: Path) -> Path:
    source = (
        Path(__file__).resolve().parents[2]
        / "src"
        / "inferyard"
        / "data"
        / "community"
        / "bundles"
        / "zh-smoke.json"
    )
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(source.read_bytes())
    return path


def _make_macos_polystrata_install(
    root: Path,
    *,
    data_dir: Path,
    with_strata: bool = True,
    with_poly: bool = False,
    with_vision: bool = False,
) -> tuple[Path, Path | None, Path | None]:
    root.mkdir(parents=True, exist_ok=True)
    (root / "serve").mkdir(exist_ok=True)
    (root / "serve" / "server.py").write_text("print('strata server')\n", encoding="utf-8")
    (root / "setup.py").write_text("print('setup')\n", encoding="utf-8")
    (root / "data").mkdir(exist_ok=True)
    (root / "data" / "expert-profile.bin").write_bytes(b"shipped-expert-profile")

    venv_dir = root / ".venv"
    venv_dir.mkdir(exist_ok=True)
    (venv_dir / "pyvenv.cfg").write_text(
        "home = /usr/bin\ninclude-system-site-packages = false\n", encoding="utf-8"
    )
    py_exe = venv_dir / "bin" / "python"
    py_exe.parent.mkdir(parents=True, exist_ok=True)
    py_exe.write_bytes(b"#!/bin/sh\n")
    py_exe.chmod(0o755)

    strata_bin: Path | None = None
    poly_bin: Path | None = None
    build_dir = root / "build-metal"
    build_dir.mkdir(exist_ok=True)
    (build_dir / "BUILD.json").write_text(json.dumps({"backend": "metal"}), encoding="utf-8")
    if with_strata:
        strata_bin = build_dir / "strata"
        strata_bin.write_bytes(b"metal-strata-" + root.name.encode("utf-8"))
        strata_bin.chmod(0o755)
    if with_poly:
        poly_bin = build_dir / "strata-poly"
        poly_bin.write_bytes(b"metal-poly-" + root.name.encode("utf-8"))
        poly_bin.chmod(0o755)
    if with_vision:
        vbin = build_dir / "strata-vision"
        vbin.write_bytes(b"metal-vision-" + root.name.encode("utf-8"))
        vbin.chmod(0o755)

    inst_doc = build_installation("polystrata", py_exe, data_dir=data_dir)
    inst_path = root / "installation.json"
    inst_path.write_bytes(json_bytes(inst_doc))
    return inst_path, strata_bin, poly_bin


def _candidate_request(
    *,
    inst_path: Path,
    model_path: Path,
    bundle_path: Path,
    out_dir: Path,
    results_dir: Path,
    context_size: int | None = None,
    served_model: str | None = None,
) -> SimpleNamespace:
    return SimpleNamespace(
        installation=inst_path,
        model_path=model_path,
        bundle_path=bundle_path,
        out=out_dir,
        output_root=results_dir,
        context_size=context_size,
        served_model=served_model,
        port=48857,
        server_pid=None,
        model_repo=None,
        model_revision=None,
        max_tokens=None,
        timeout_seconds=600,
        prompt_cache=None,
        temperature_stop_override_reason=None,
    )


def test_cross_installation_strata_config_rebinds_worker_executable_and_passes_worker_check(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr(installation, "_FILE_CACHE", {})
    monkeypatch.setattr("platform.system", lambda: "Darwin")
    monkeypatch.setattr("platform.machine", lambda: "arm64")
    monkeypatch.setattr(
        "inferyard.config.local_engines.candidate.environment_snapshot",
        lambda: {"ac_online": True, "profile": "macos-low-power:0"},
    )

    data_dir = tmp_path / "models" / "polystrata-data"
    pack_dir = data_dir / "packs" / "q2_0"
    tok_dir = pack_dir / "tokenizer"
    tok_dir.mkdir(parents=True)
    (tok_dir / "vocab.json").write_text("{}", encoding="utf-8")
    (tok_dir / "chat_template.jinja").write_text("{{ messages }}", encoding="utf-8")
    (pack_dir / "pack.bin").write_bytes(b"pack-data")
    mtp_dir = data_dir / "mtp" / "rt"
    mtp_dir.mkdir(parents=True)
    (mtp_dir / "mtp.bin").write_bytes(b"mtp")

    inst_old, strata_old, _ = _make_macos_polystrata_install(
        tmp_path / "workspace-old" / "engines" / "polystrata",
        data_dir=data_dir,
        with_strata=True,
        with_vision=True,
    )
    _, strata_new, _ = _make_macos_polystrata_install(
        tmp_path / "workspace" / "engines" / "polystrata",
        data_dir=data_dir,
        with_strata=True,
        with_vision=True,
    )
    assert strata_old is not None and strata_new is not None
    new_root = strata_new.parent.parent

    # Config generated in new_root, while Studio/CLI selects inst_old (default context & served_model=None).
    cfg_new = new_root / "strata-q2_0.json"
    cfg_new.write_text(
        json.dumps(
            {
                "exe": str(strata_new),
                "args": [
                    "--pack",
                    str(pack_dir),
                    "--expert-profile",
                    str(new_root / "data" / "expert-profile.bin"),
                    "--mtp",
                    str(mtp_dir),
                    "--max-context",
                    "32768",
                ],
                "cwd": str(new_root),
                "tokenizer": str(tok_dir),
                "model_name": "qwen3.8-flash-next-q2_0",
                "log": str(new_root / "strata-q2_0.log"),
                "backend": "metal",
                "env": {"STRATA_METAL_STREAM": "0"},
                "vision": {
                    "exe": str(strata_new.parent / "strata-vision"),
                    "gpu": True,
                },
            }
        ),
        encoding="utf-8",
    )

    bundle = _write_bundle(tmp_path / "bundle.json")
    details = create_candidate(
        _candidate_request(
            inst_path=inst_old,
            model_path=cfg_new,
            bundle_path=bundle,
            out_dir=tmp_path / "cand-cross",
            results_dir=tmp_path / "results",
            context_size=None,
            served_model=None,
        )
    )

    derived_path = Path(details["polystrata_config"])
    assert derived_path.name == "strata-candidate.json"
    derived_cfg = json.loads(derived_path.read_text(encoding="utf-8"))
    old_root = strata_old.parent.parent.resolve()
    assert derived_cfg["exe"] == str(strata_old.resolve())
    assert derived_cfg["cwd"] == str(old_root)
    assert "log" not in derived_cfg
    assert derived_cfg["vision"]["exe"] == str((strata_old.parent / "strata-vision").resolve())
    ep_idx = derived_cfg["args"].index("--expert-profile")
    assert derived_cfg["args"][ep_idx + 1] == str((old_root / "data" / "expert-profile.bin").resolve())

    # Verify assets() and observe_workers() accept the rebound worker executable.
    loaded_cfg = load_config(details["candidate"]).config.to_dict()
    _, runtime_files, _ = verify_assets(loaded_cfg)

    from test_polystrata_macos_workers import _make_fake_env

    fake_psutil = _make_fake_env(
        monkeypatch,
        root_pid=500,
        children_seq=[{600}, {600}],
        proc_specs={
            600: {
                "ppid": 500,
                "start": 2001,
                "args": [derived_cfg["exe"], "--serve", *derived_cfg["args"]],
                "exe": Path(derived_cfg["exe"]).resolve(),
                "cwd": Path(derived_cfg["cwd"]).resolve(),
                "env": {"PYTHONDONTWRITEBYTECODE": "1", "STRATA_METAL_STREAM": "0"},
            }
        },
    )
    observed = workers.observe_workers(
        500,
        fake_psutil,
        runtime_files,
        {"PYTHONDONTWRITEBYTECODE": "1"},
        family="polystrata",
        allowed_extras=frozenset({"STRATA_METAL_STREAM"}),
        expected_cfg_env={"STRATA_METAL_STREAM": "0"},
    )
    assert [row["pid"] for row in observed] == [600]


def test_migrated_config_after_uninstall_rebinds_and_missing_role_fails_fast(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr(installation, "_FILE_CACHE", {})
    monkeypatch.setattr("platform.system", lambda: "Darwin")
    monkeypatch.setattr("platform.machine", lambda: "arm64")
    monkeypatch.setattr(
        "inferyard.config.local_engines.candidate.environment_snapshot",
        lambda: {"ac_online": True, "profile": "macos-low-power:0"},
    )

    data_dir = tmp_path / "models" / "polystrata-data"
    pack_dir = data_dir / "packs" / "q2_0"
    tok_dir = pack_dir / "tokenizer"
    tok_dir.mkdir(parents=True)
    (tok_dir / "vocab.json").write_text("{}", encoding="utf-8")
    (tok_dir / "chat_template.jinja").write_text("{{ messages }}", encoding="utf-8")
    (pack_dir / "pack.bin").write_bytes(b"pack-data")

    removed_root = tmp_path / "removed-polystrata"
    migrated_cfg = data_dir / "strata-q2_0.json"
    migrated_cfg.write_text(
        json.dumps(
            {
                "exe": str(removed_root / "build-metal" / "strata"),
                "args": [
                    "--pack",
                    "packs/q2_0",
                    "--expert-profile",
                    str(removed_root / "data" / "expert-profile.bin"),
                    "--max-context",
                    "32768",
                ],
                "tokenizer": "packs/q2_0/tokenizer",
                "model_name": "qwen3.8-flash-next-q2_0",
            }
        ),
        encoding="utf-8",
    )
    if removed_root.exists():
        shutil.rmtree(removed_root)

    inst_active, strata_active, _ = _make_macos_polystrata_install(
        tmp_path / "active-polystrata",
        data_dir=data_dir,
        with_strata=True,
    )
    assert strata_active is not None
    bundle = _write_bundle(tmp_path / "bundle.json")

    details = create_candidate(
        _candidate_request(
            inst_path=inst_active,
            model_path=migrated_cfg,
            bundle_path=bundle,
            out_dir=tmp_path / "cand-migrated",
            results_dir=tmp_path / "results-mig",
        )
    )
    derived = json.loads(Path(details["polystrata_config"]).read_text(encoding="utf-8"))
    active_root = strata_active.parent.parent.resolve()
    assert derived["exe"] == str(strata_active.resolve())
    assert derived["cwd"] == str(active_root)
    assert derived["tokenizer"] == str(tok_dir.resolve())
    ep_idx = derived["args"].index("--expert-profile")
    assert derived["args"][ep_idx + 1] == str((active_root / "data" / "expert-profile.bin").resolve())

    # Installation that only has strata-poly must reject a pack config needing strata at prepare time.
    inst_poly_only, _, _ = _make_macos_polystrata_install(
        tmp_path / "poly-only",
        data_dir=data_dir,
        with_strata=False,
        with_poly=True,
    )
    with pytest.raises(PreparationError, match="local_engine_polystrata_engine_missing"):
        create_candidate(
            _candidate_request(
                inst_path=inst_poly_only,
                model_path=migrated_cfg,
                bundle_path=bundle,
                out_dir=tmp_path / "cand-missing-strata",
                results_dir=tmp_path / "results-missing",
            )
        )


def test_legacy_top_level_pack_key_with_args_lib_dirs_backend_conflict_and_temp_bounds(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setattr(installation, "_FILE_CACHE", {})
    monkeypatch.setattr("platform.system", lambda: "Linux")
    monkeypatch.setattr("platform.machine", lambda: "x86_64")
    monkeypatch.setattr(
        "inferyard.config.local_engines.candidate.environment_snapshot",
        lambda: {"ac_online": None},
    )

    data_dir = tmp_path / "models" / "polystrata-data"
    pack_dir = data_dir / "packs" / "q2_0"
    tok_dir = pack_dir / "tokenizer"
    tok_dir.mkdir(parents=True)
    (tok_dir / "vocab.json").write_text("{}", encoding="utf-8")
    (tok_dir / "chat_template.jinja").write_text("{{ messages }}", encoding="utf-8")
    (pack_dir / "pack.bin").write_bytes(b"pack-data")

    def _make_linux_install(root: Path, backend: str, rel_libs: list[str]) -> tuple[Path, Path]:
        root.mkdir(parents=True, exist_ok=True)
        (root / "serve").mkdir(exist_ok=True)
        (root / "serve" / "server.py").write_text("print('strata server')\n", encoding="utf-8")
        (root / "setup.py").write_text("print('setup')\n", encoding="utf-8")
        venv = root / ".venv"
        venv.mkdir(exist_ok=True)
        (venv / "pyvenv.cfg").write_text("home = /usr/bin\n", encoding="utf-8")
        py_exe = venv / "bin" / "python"
        py_exe.parent.mkdir(parents=True, exist_ok=True)
        py_exe.write_bytes(b"#!/bin/sh\n")
        py_exe.chmod(0o755)
        eng_dir = root / "engine"
        eng_dir.mkdir(exist_ok=True)
        for rel in rel_libs:
            (eng_dir / rel).mkdir(parents=True, exist_ok=True)
        (eng_dir / "BUILD.json").write_text(
            json.dumps({"backend": backend, "lib_dirs": rel_libs}), encoding="utf-8"
        )
        sbin = eng_dir / "strata"
        sbin.write_bytes(b"linux-strata-" + backend.encode("utf-8"))
        sbin.chmod(0o755)
        inst_path = root / "installation.json"
        inst_path.write_bytes(json_bytes(build_installation("polystrata", py_exe, data_dir=data_dir)))
        return inst_path, sbin

    inst_hip, strata_hip = _make_linux_install(tmp_path / "hip-inst", "hip", ["rocm-libs"])
    _, strata_other = _make_linux_install(tmp_path / "other-hip", "hip", ["old-libs"])
    ext_lib = tmp_path / "external-toolkit-lib"
    ext_lib.mkdir()

    # Legacy top-level pack_dir + args list + tokenizer_dir + lib_dirs rebind must pass verify_assets().
    legacy_cfg = strata_other.parent.parent / "strata-legacy.json"
    legacy_cfg.write_text(
        json.dumps(
            {
                "exe": str(strata_other),
                "pack_dir": "packs/q2_0",
                "tokenizer_dir": "packs/q2_0/tokenizer",
                "args": ["--max-context", "16384"],
                "cwd": str(strata_other.parent.parent),
                "backend": "hip",
                "lib_dirs": [
                    str(strata_other.parent / "old-libs"),
                    str(ext_lib),
                ],
                "model_name": "legacy-pack",
            }
        ),
        encoding="utf-8",
    )
    bundle = _write_bundle(tmp_path / "bundle.json")
    details = create_candidate(
        _candidate_request(
            inst_path=inst_hip,
            model_path=legacy_cfg,
            bundle_path=bundle,
            out_dir=tmp_path / "cand-legacy",
            results_dir=tmp_path / "results-legacy",
        )
    )
    loaded_cfg = load_config(details["candidate"]).config.to_dict()
    verify_assets(loaded_cfg)
    derived = json.loads(Path(details["polystrata_config"]).read_text(encoding="utf-8"))
    assert derived["pack_dir"] == str(pack_dir.resolve())
    assert derived["tokenizer"] == str(tok_dir.resolve())
    assert derived["lib_dirs"] == [
        str((strata_hip.parent / "rocm-libs").resolve()),
        str(ext_lib.resolve()),
    ]
    assert loaded_cfg["engine"]["backend"] == "hip"
    assert loaded_cfg["safety"]["max_temperature_celsius"] == 85

    # Non-Metal candidate must reject max_temperature_celsius > 85; Metal accepts 115 and rejects 116.
    bad_non_metal = json.loads(json.dumps(loaded_cfg))
    bad_non_metal["safety"]["max_temperature_celsius"] = 86
    with pytest.raises(ContractError):
        validate_document("config", bad_non_metal)

    metal_ok = json.loads(json.dumps(loaded_cfg))
    metal_ok["engine"]["backend"] = "metal"
    metal_ok["safety"]["require_temperature"] = True
    metal_ok["safety"]["max_temperature_celsius"] = 115
    validate_document("config", metal_ok)

    metal_bad = json.loads(json.dumps(metal_ok))
    metal_bad["safety"]["max_temperature_celsius"] = 116
    with pytest.raises(ContractError):
        validate_document("config", metal_bad)

    # Explicit CUDA config rebound onto HIP installation must fail fast with polystrata_backend_unsupported.
    cuda_cfg = strata_other.parent.parent / "strata-cuda.json"
    cuda_cfg.write_text(
        json.dumps(
            {
                "exe": str(strata_other),
                "args": ["--pack", str(pack_dir), "--max-context", "16384"],
                "cwd": str(strata_other.parent.parent),
                "tokenizer": str(tok_dir),
                "backend": "cuda",
                "model_name": "cuda-pack",
            }
        ),
        encoding="utf-8",
    )
    with pytest.raises(PreparationError, match="polystrata_backend_unsupported"):
        create_candidate(
            _candidate_request(
                inst_path=inst_hip,
                model_path=cuda_cfg,
                bundle_path=bundle,
                out_dir=tmp_path / "cand-conflict",
                results_dir=tmp_path / "results-conflict",
            )
        )
