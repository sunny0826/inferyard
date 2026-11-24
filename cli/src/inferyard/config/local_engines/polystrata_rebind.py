"""Rebind PolyStrata model JSON configs to the selected installation runtime."""

from __future__ import annotations

import json
from collections.abc import Callable
from pathlib import Path

from inferyard.platforms.local_engines.detect_polystrata import (
    _is_usable_binary,
    find_polystrata_engine_binaries,
)
from inferyard.provisioning.preparation_io import PreparationError

_PATH_FLAGS = frozenset(
    {
        "--model",
        "--native",
        "--pack",
        "--ple-gguf",
        "--embd-gguf",
        "--draft-model",
        "--mmproj",
    }
)


def _in_roots(path: Path, roots: tuple[Path, ...]) -> bool:
    return any(path.is_relative_to(root) for root in roots)


def _is_int_text(value: str) -> bool:
    try:
        int(value.strip())
    except ValueError:
        return False
    return True


def _runtime_roots(installation: dict) -> tuple[Path, ...]:
    resolved: list[Path] = []
    for raw in installation.get("runtime_roots") or ():
        if not isinstance(raw, str) or not raw.strip():
            continue
        cand = Path(raw)
        if cand.exists() and not cand.is_symlink():
            resolved.append(cand.resolve(strict=True))
    return tuple(resolved)


def _resolve_target_exe(
    payload: dict,
    inst_root: Path,
    runtime_roots: tuple[Path, ...],
    orig_bases: tuple[Path, ...],
    *,
    is_poly: bool,
    resolve_ref: Callable[[str, tuple[Path, ...]], Path | None],
    system: str,
) -> tuple[Path, bool]:
    exe_raw = payload.get("exe")
    if isinstance(exe_raw, str) and exe_raw.strip():
        resolved_exe = resolve_ref(exe_raw.strip(), orig_bases)
        if resolved_exe is not None and _in_roots(resolved_exe, runtime_roots):
            return resolved_exe, False
    binaries = find_polystrata_engine_binaries(inst_root, platform_system=system)
    role = "poly" if is_poly else "strata"
    candidate = binaries.get(role)
    if candidate is None or not _in_roots(candidate, runtime_roots):
        raise PreparationError("local_engine_polystrata_engine_missing")
    return candidate, True


def _rebind_vision(
    payload: dict,
    target_exe: Path,
    runtime_roots: tuple[Path, ...],
    orig_bases: tuple[Path, ...],
    *,
    resolve_ref: Callable[[str, tuple[Path, ...]], Path | None],
    system: str,
) -> tuple[dict | None, bool]:
    raw_vision = payload.get("vision")
    if not isinstance(raw_vision, dict):
        return None, False
    vdict = dict(raw_vision)
    for key in ("mmproj", "model"):
        val = vdict.get(key)
        if isinstance(val, str) and val.strip():
            resolved = resolve_ref(val.strip(), orig_bases)
            if resolved is not None:
                vdict[key] = str(resolved)
    vexe_raw = vdict.get("exe")
    if not (isinstance(vexe_raw, str) and vexe_raw.strip()):
        return vdict, False
    resolved_vexe = resolve_ref(vexe_raw.strip(), orig_bases)
    if resolved_vexe is not None and _in_roots(resolved_vexe, runtime_roots):
        vdict["exe"] = str(resolved_vexe)
        return vdict, False
    vexe_name = "strata-vision.exe" if system == "Windows" else "strata-vision"
    for cand in (target_exe.parent / vexe_name, *(r / vexe_name for r in runtime_roots)):
        if not cand.parent.is_symlink() and _is_usable_binary(cand, system):
            res = cand.resolve(strict=True)
            if _in_roots(res, runtime_roots):
                vdict["exe"] = str(res)
                return vdict, True
    raise PreparationError("local_engine_polystrata_engine_missing")


def _resolve_expert_profile(
    raw_val: str,
    orig_bases: tuple[Path, ...],
    inst_root: Path,
    data_dir: Path | None,
    *,
    exe_rebound: bool,
    resolve_ref: Callable[[str, tuple[Path, ...]], Path | None],
) -> tuple[str, bool]:
    trimmed = raw_val.strip()
    if not trimmed:
        return raw_val, False
    resolved_ep = resolve_ref(trimmed, orig_bases)
    inst_ep = inst_root / "data" / Path(trimmed).name
    inst_ep_ok = inst_ep.is_file() and not inst_ep.is_symlink()
    if resolved_ep is None:
        if inst_ep_ok:
            return str(inst_ep.resolve(strict=True)), True
        return raw_val, False
    in_inst = resolved_ep.is_relative_to(inst_root)
    in_data = data_dir is not None and resolved_ep.is_relative_to(data_dir)
    if not in_inst and not in_data and exe_rebound and resolved_ep.parent.name == "data" and inst_ep_ok:
        return str(inst_ep.resolve(strict=True)), True
    return str(resolved_ep), False


def _rewrite_args(
    raw_args: list,
    orig_bases: tuple[Path, ...],
    inst_root: Path,
    data_dir: Path | None,
    *,
    exe_rebound: bool,
    resolve_ref: Callable[[str, tuple[Path, ...]], Path | None],
) -> tuple[list[str], bool]:
    out = [str(x) for x in raw_args]
    ep_rebound = False
    idx = 0
    while idx < len(out):
        item = out[idx]
        flag, sep, inline_val = item.partition("=")
        has_next = not sep and idx + 1 < len(out)
        val = inline_val if sep else (out[idx + 1] if has_next else None)
        if val is not None:
            if flag in _PATH_FLAGS or (flag == "--mtp" and not _is_int_text(val)):
                resolved = resolve_ref(val.strip(), orig_bases) if val.strip() else None
                if resolved is not None:
                    if sep:
                        out[idx] = f"{flag}={resolved}"
                    else:
                        out[idx + 1] = str(resolved)
                if not sep:
                    idx += 2
                    continue
            elif flag == "--expert-profile":
                new_val, changed = _resolve_expert_profile(
                    val,
                    orig_bases,
                    inst_root,
                    data_dir,
                    exe_rebound=exe_rebound,
                    resolve_ref=resolve_ref,
                )
                ep_rebound = ep_rebound or changed
                if sep:
                    out[idx] = f"{flag}={new_val}"
                else:
                    out[idx + 1] = new_val
                    idx += 2
                    continue
        idx += 1
    return out, ep_rebound


def _rebind_lib_dirs(payload: dict, target_exe: Path, inst_root: Path) -> list[str]:
    result: list[str] = []
    build_json = target_exe.parent / "BUILD.json"
    if build_json.is_file() and not build_json.is_symlink():
        try:
            meta = json.loads(build_json.read_text(encoding="utf-8"))
        except (OSError, ValueError, UnicodeError):
            meta = None
        if isinstance(meta, dict) and isinstance(meta.get("lib_dirs"), list):
            for rel in meta["lib_dirs"]:
                if isinstance(rel, str) and rel.strip():
                    cand = (target_exe.parent / rel.strip())
                    if cand.is_dir() and not cand.is_symlink():
                        s = str(cand.resolve(strict=True))
                        if s not in result:
                            result.append(s)
    orig_checkout: Path | None = None
    raw_cwd = payload.get("cwd")
    if isinstance(raw_cwd, str) and raw_cwd.strip() and Path(raw_cwd.strip()).is_absolute():
        orig_checkout = Path(raw_cwd.strip())
    elif isinstance(payload.get("exe"), str) and Path(payload["exe"].strip()).is_absolute():
        orig_checkout = Path(payload["exe"].strip()).parent.parent
    for entry in payload.get("lib_dirs") or ():
        if not isinstance(entry, str) or not entry.strip():
            continue
        p = Path(entry.strip())
        if not p.is_dir() or p.is_symlink():
            continue
        res = p.resolve(strict=True)
        if (
            orig_checkout is not None
            and orig_checkout != inst_root
            and (p.is_relative_to(orig_checkout) or (orig_checkout.exists() and res.is_relative_to(orig_checkout.resolve())))
        ):
            continue
        s = str(res)
        if s not in result:
            result.append(s)
    return result


def rebind_or_keep_polystrata_config(
    payload: dict,
    installation: dict,
    out_dir: Path,
    orig_bases: tuple[Path, ...],
    *,
    safe_keys: tuple[str, ...],
    legacy_model_keys: tuple[str, ...],
    tokenizer_keys: tuple[str, ...],
    model_asset: Path,
    model_flag: str | None,
    existing_ctx: int | None,
    resolved_context: int,
    context_size: int | None,
    json_model_name: str | None,
    resolved_served: str,
    served_model: str | None,
    is_poly: bool,
    resolve_ref: Callable[[str, tuple[Path, ...]], Path | None],
    set_arg_value: Callable[[list[str], str, str], list[str]],
    system: str,
) -> tuple[Path | None, dict | None, Path]:
    """Return (derived_config_path_or_None, derived_payload_or_None, target_exe)."""
    inst_root = Path(installation["working_directory"]).resolve(strict=True)
    data_dir: Path | None = None
    if installation.get("data_dir"):
        dd = Path(installation["data_dir"])
        if dd.is_dir() and not dd.is_symlink():
            data_dir = dd.resolve(strict=True)
    runtime_roots = _runtime_roots(installation)
    target_exe, exe_rebound = _resolve_target_exe(
        payload,
        inst_root,
        runtime_roots,
        orig_bases,
        is_poly=is_poly,
        resolve_ref=resolve_ref,
        system=system,
    )
    rebound_vision, vision_rebound = _rebind_vision(
        payload,
        target_exe,
        runtime_roots,
        orig_bases,
        resolve_ref=resolve_ref,
        system=system,
    )
    cwd_mismatch = False
    raw_cwd = payload.get("cwd")
    if isinstance(raw_cwd, str) and raw_cwd.strip():
        cwd_p = Path(raw_cwd.strip())
        if not cwd_p.is_dir() or cwd_p.is_symlink() or cwd_p.resolve(strict=True) != inst_root:
            cwd_mismatch = True

    rewritten_args: list[str] | None = None
    ep_rebound = False
    if isinstance(payload.get("args"), list):
        rewritten_args, ep_rebound = _rewrite_args(
            payload["args"],
            orig_bases,
            inst_root,
            data_dir,
            exe_rebound=exe_rebound,
            resolve_ref=resolve_ref,
        )

    needs_derive = (
        (context_size is not None and existing_ctx != context_size)
        or (served_model is not None and served_model != json_model_name)
        or exe_rebound
        or vision_rebound
        or cwd_mismatch
        or ep_rebound
    )
    if not needs_derive:
        return None, None, target_exe

    derived = {k: payload[k] for k in safe_keys if k in payload}
    derived["exe"] = str(target_exe)
    derived["cwd"] = str(inst_root)
    derived["model_name"] = resolved_served
    if rebound_vision is not None:
        derived["vision"] = rebound_vision
    if exe_rebound and ("lib_dirs" in derived or (target_exe.parent / "BUILD.json").is_file()):
        new_libs = _rebind_lib_dirs(payload, target_exe, inst_root)
        if new_libs or "lib_dirs" in derived:
            derived["lib_dirs"] = new_libs
    if rewritten_args is not None:
        new_args = list(rewritten_args)
        if model_flag and model_flag.startswith("--"):
            new_args = set_arg_value(new_args, model_flag, str(model_asset))
        new_args = set_arg_value(new_args, "--max-context", str(resolved_context))
        derived["args"] = new_args
    else:
        derived["context"] = resolved_context
        if model_flag and model_flag.startswith("key:"):
            derived[model_flag[4:]] = str(model_asset)
    for key in (*legacy_model_keys, *tokenizer_keys):
        val = derived.get(key)
        if isinstance(val, str) and val.strip():
            resolved = resolve_ref(val.strip(), orig_bases)
            if resolved is not None:
                derived[key] = str(resolved)
    return out_dir / "strata-candidate.json", derived, target_exe
