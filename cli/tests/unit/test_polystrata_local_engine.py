"""Unit tests for PolyStrata plaintext credential rejection and GGUF arch enforcement."""

from __future__ import annotations

import json
import struct
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.config.loader import load_config
from inferyard.config.local_engines.candidate import create as create_candidate
from inferyard.contracts.validation import json_bytes
from inferyard.platforms.local_engines import installation
from inferyard.platforms.local_engines.detect import build_installation
from inferyard.provisioning.preparation_io import PreparationError


def _write_gguf(path: Path, metadata: dict[str, str | int], payload: bytes = b"tensors") -> Path:
    parts = [struct.pack("<4sIQQ", b"GGUF", 3, 1, len(metadata))]
    for key, value in metadata.items():
        key_bytes = key.encode("utf-8")
        parts.append(struct.pack("<Q", len(key_bytes)) + key_bytes)
        if isinstance(value, str):
            val_bytes = value.encode("utf-8")
            parts.append(struct.pack("<IQ", 8, len(val_bytes)) + val_bytes)
        else:
            parts.append(struct.pack("<II", 4, value))
    parts.append(payload)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(b"".join(parts))
    return path


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


def _create_polystrata_layout(
    root: Path,
    *,
    platform_kind: str = "linux",
    build_json: dict | None = None,
) -> tuple[Path, Path, Path]:
    root.mkdir(parents=True, exist_ok=True)
    (root / "serve").mkdir(exist_ok=True)
    (root / "serve" / "server.py").write_text("print('strata server')\n", encoding="utf-8")
    (root / "setup.py").write_text("print('setup')\n", encoding="utf-8")

    venv_dir = root / ".venv"
    venv_dir.mkdir(exist_ok=True)
    (venv_dir / "pyvenv.cfg").write_text(
        "home = /usr/bin\ninclude-system-site-packages = false\n", encoding="utf-8"
    )
    exe = venv_dir / "bin" / "python"
    engine_bin = root / "engine-poly" / "strata-poly"
    strata_bin = root / "engine" / "strata"
    exe.parent.mkdir(parents=True, exist_ok=True)
    exe.write_bytes(b"#!/bin/sh\n")
    exe.chmod(0o755)
    for binary in (engine_bin, strata_bin):
        binary.parent.mkdir(parents=True, exist_ok=True)
        binary.write_bytes(b"synthetic-engine-binary")
        binary.chmod(0o755)

    if build_json is not None:
        (engine_bin.parent / "BUILD.json").write_text(json.dumps(build_json), encoding="utf-8")
    (root / "data").mkdir(exist_ok=True)
    return root, exe, engine_bin


def _candidate_request(
    *,
    inst_path: Path,
    model_path: Path,
    bundle_path: Path,
    out_dir: Path,
    results_dir: Path,
    context_size: int | None = None,
    port: int = 48857,
) -> SimpleNamespace:
    return SimpleNamespace(
        installation=inst_path,
        model_path=model_path,
        bundle_path=bundle_path,
        out=out_dir,
        output_root=results_dir,
        context_size=context_size,
        served_model="qwen36-local",
        port=port,
        server_pid=None,
        model_repo="unsloth/Qwen3.6-35B-A3B-MTP-GGUF",
        model_revision=None,
        max_tokens=512,
        timeout_seconds=600,
        prompt_cache=None,
        temperature_stop_override_reason=None,
    )


def test_upstream_pack_config_credential_rejection_and_arch_enforcement(
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

    ps_root, exe, engine_bin = _create_polystrata_layout(
        tmp_path / "ps-linux",
        platform_kind="linux",
        build_json={"backend": "cuda"},
    )
    inst_path = tmp_path / "inst.json"
    inst_path.write_bytes(json_bytes(build_installation("polystrata", exe)))
    bundle = _write_bundle(tmp_path / "bundle.json")

    bad_gguf = _write_gguf(
        tmp_path / "llama3.gguf",
        {
            "general.name": "Llama-3-8B",
            "general.architecture": "llama",
            "tokenizer.chat_template": "{{ messages }}",
        },
    )
    with pytest.raises(PreparationError, match="polystrata_gguf_architecture_unsupported"):
        create_candidate(
            _candidate_request(
                inst_path=inst_path,
                model_path=bad_gguf,
                bundle_path=bundle,
                out_dir=tmp_path / "cand-bad-direct",
                results_dir=tmp_path / "res-bad-direct",
            )
        )
    bad_json = ps_root / "data" / "strata-llama.json"
    bad_json.write_text(
        json.dumps(
            {
                "exe": str(engine_bin),
                "args": ["--model", str(bad_gguf), "--max-context", "8192"],
                "cwd": str(ps_root),
            }
        ),
        encoding="utf-8",
    )
    with pytest.raises(PreparationError, match="polystrata_gguf_architecture_unsupported"):
        create_candidate(
            _candidate_request(
                inst_path=inst_path,
                model_path=bad_json,
                bundle_path=bundle,
                out_dir=tmp_path / "cand-bad-json",
                results_dir=tmp_path / "res-bad-json",
            )
        )

    good_gguf = _write_gguf(
        tmp_path / "qwen36.gguf",
        {
            "general.name": "Qwen3.6",
            "general.architecture": "qwen35moe",
            "tokenizer.chat_template": "{{ messages }}",
        },
    )
    secret_json = ps_root / "data" / "strata-secret.json"
    secret_json.write_text(
        json.dumps(
            {
                "exe": str(engine_bin),
                "args": ["--model", str(good_gguf), "--max-context", "32768"],
                "cwd": str(ps_root),
                "api_key": "synthetic-secret-token",
            }
        ),
        encoding="utf-8",
    )
    with pytest.raises(PreparationError, match="polystrata_plaintext_credential_rejected"):
        create_candidate(
            _candidate_request(
                inst_path=inst_path,
                model_path=secret_json,
                bundle_path=bundle,
                out_dir=tmp_path / "cand-secret",
                results_dir=tmp_path / "res-secret",
                context_size=65536,
            )
        )

    pack_dir = tmp_path / "packs" / "iq2_xs"
    tok_dir = pack_dir / "tokenizer"
    tok_dir.mkdir(parents=True)
    (tok_dir / "vocab.json").write_text("{}", encoding="utf-8")
    (tok_dir / "chat_template.jinja").write_text("{{ messages }}", encoding="utf-8")
    (pack_dir / "weights.bin").write_bytes(b"pack-weights")
    upstream_pack_json = ps_root / "data" / "strata-iq2_xs.json"
    upstream_pack_json.write_text(
        json.dumps(
            {
                "exe": str(ps_root / "engine" / "strata"),
                "args": ["--pack", str(pack_dir)],
                "cwd": str(ps_root),
                "tokenizer": str(tok_dir),
                "model_name": "qwen3.8-flash-next-iq2_xs",
            }
        ),
        encoding="utf-8",
    )
    details = create_candidate(
        _candidate_request(
            inst_path=inst_path,
            model_path=upstream_pack_json,
            bundle_path=bundle,
            out_dir=tmp_path / "cand-pack",
            results_dir=tmp_path / "res-pack",
            context_size=None,
        )
    )
    loaded = load_config(details["candidate"]).config.to_dict()
    assert loaded["model"]["kind"] == "directory"
    assert loaded["model"]["local_path"] == str(pack_dir.resolve())
    assert loaded["conditions"]["context_size"] == 4096
    manifest_files = {
        row["path"]
        for row in json.loads(Path(loaded["model"]["assets_path"]).read_text(encoding="utf-8"))[
            "files"
        ]
    }
    assert "weights.bin" in manifest_files

    mcp_secret_json = ps_root / "data" / "strata-mcp-secret.json"
    mcp_secret_json.write_text(
        json.dumps(
            {
                "exe": str(engine_bin),
                "args": ["--model", str(good_gguf), "--max-context", "32768"],
                "cwd": str(ps_root),
                "mcp_servers": {"remote": {"headers": {"Authorization": "Bearer secret"}}},
            }
        ),
        encoding="utf-8",
    )
    with pytest.raises(PreparationError, match="polystrata_plaintext_credential_rejected"):
        create_candidate(
            _candidate_request(
                inst_path=inst_path,
                model_path=mcp_secret_json,
                bundle_path=bundle,
                out_dir=tmp_path / "cand-mcp-secret",
                results_dir=tmp_path / "res-mcp-secret",
                context_size=65536,
            )
        )

    cwd_gguf = _write_gguf(
        ps_root / "shared.gguf",
        {
            "general.name": "CwdModel",
            "general.architecture": "qwen35moe",
            "tokenizer.chat_template": "{{ messages }}",
        },
        payload=b"cwd-copy",
    )
    _write_gguf(
        ps_root / "data" / "shared.gguf",
        {
            "general.name": "DataModel",
            "general.architecture": "qwen35moe",
            "tokenizer.chat_template": "{{ messages }}",
        },
        payload=b"data-copy",
    )
    rel_json = ps_root / "data" / "strata-rel.json"
    rel_json.write_text(
        json.dumps(
            {
                "exe": str(engine_bin),
                "args": ["--model", "shared.gguf", "--max-context", "16384"],
                "cwd": str(ps_root),
                "model_name": "qwen36-local",
            }
        ),
        encoding="utf-8",
    )
    rel_details = create_candidate(
        _candidate_request(
            inst_path=inst_path,
            model_path=rel_json,
            bundle_path=bundle,
            out_dir=tmp_path / "cand-rel",
            results_dir=tmp_path / "res-rel",
            context_size=None,
        )
    )
    assert Path(rel_details["model"]) == cwd_gguf.resolve()

    details_override = create_candidate(
        _candidate_request(
            inst_path=inst_path,
            model_path=upstream_pack_json,
            bundle_path=bundle,
            out_dir=tmp_path / "cand-pack-override",
            results_dir=tmp_path / "res-pack-override",
            context_size=65536,
        )
    )
    override_cfg = json.loads(
        Path(details_override["polystrata_config"]).read_text(encoding="utf-8")
    )
    assert "--max-context" in override_cfg["args"]
    idx = override_cfg["args"].index("--max-context")
    assert override_cfg["args"][idx + 1] == "65536"
