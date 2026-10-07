"""Verify a prepared runtime against installed trusted metadata, never receipt-supplied hashes."""

import os
import re
import stat
import subprocess
from pathlib import Path

from inferyard.config.preparation_io import PreparationError, file_record, linked, read_json
from inferyard.platforms.runtime_archive import safe_member
from inferyard.platforms.runtime_profiles import exact_value, load_profile, output_files


def inside_file(root, relative):
    safe_member(relative)
    root = Path(root).absolute()
    if linked(root) or not root.is_dir():
        raise PreparationError("runtime_asset_mismatch")
    current = root
    for part in relative.split("/"):
        current /= part
        if linked(current):
            raise PreparationError("runtime_asset_mismatch")
    if not stat.S_ISREG(current.stat().st_mode) or not current.resolve().is_relative_to(
        root.resolve()
    ):
        raise PreparationError("runtime_asset_mismatch")
    return current


def verify_files(root, profile, *, metadata=False):
    _, manifest, _, expected = output_files(profile["members"])
    allowed = set(expected)
    if metadata:
        allowed |= {manifest, "runtime-receipt.json"}
    actual = set()
    for parent, directories, files in os.walk(root, followlinks=False):
        for name in [*directories, *files]:
            path = Path(parent) / name
            if linked(path):
                raise PreparationError("runtime_asset_mismatch")
        actual.update((Path(parent) / n).relative_to(root).as_posix() for n in files)
    # The receipt is absent until success; all asset/manifest paths must be exact.
    required = set(expected) | ({manifest} if metadata else set())
    if not required <= actual or actual - allowed:
        raise PreparationError("runtime_asset_mismatch")
    for relative, member in expected.items():
        record = file_record(inside_file(root, relative))
        if (record["sha256"], record["bytes"]) != (member["sha256"], member["bytes"]):
            raise PreparationError("runtime_asset_mismatch")


def query_version(engine, pattern, *, timeout=10):
    try:
        result = subprocess.run(
            [str(engine), "--version"],
            cwd=Path(engine).parent,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=timeout,
            check=False,
            **({"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}),
        )
    except subprocess.TimeoutExpired:
        raise PreparationError("engine_version_query_failed", 4) from None
    if result.returncode != 0:
        raise PreparationError("engine_version_query_failed", 4)
    if not re.search(pattern, result.stdout + result.stderr):
        raise PreparationError("unsupported_engine_build_requires_adapter_validation")
    return {
        "argv": ["--version"],
        "returncode": 0,
        "stdout": result.stdout,
        "stderr": result.stderr,
    }


def verify_receipt(path, *, mode=None):
    path = Path(path).absolute()
    if linked(path):
        raise PreparationError("runtime_receipt_invalid")
    receipt = read_json(path, "runtime_receipt_invalid")
    keys = {
        "kind",
        "schema_version",
        "profile",
        "profile_sha256",
        "platform",
        "architecture",
        "mode",
        "engine_release",
        "archives",
        "engine_binary",
        "runtime_library_manifest",
        "engine_sha256",
        "libraries",
        "version",
        "model_requests_sent",
        "ready_to_run",
    }
    if type(receipt) is not dict or set(receipt) != keys or type(receipt.get("profile")) is not str:
        raise PreparationError("runtime_receipt_invalid")
    profile, digest = load_profile(receipt["profile"])
    constant = dict(
        kind="community_runtime_receipt.v1",
        schema_version=3,
        profile_sha256=digest,
        model_requests_sent=0,
        ready_to_run=False,
        engine_sha256=profile["libraries"]["llama-server.exe"],
    )
    constant.update(
        {
            key: profile[key]
            for key in (
                "profile",
                "platform",
                "architecture",
                "mode",
                "engine_release",
                "archives",
                "engine_binary",
                "runtime_library_manifest",
                "libraries",
            )
        }
    )
    if any(not exact_value(receipt[key], value) for key, value in constant.items()):
        raise PreparationError("runtime_profile_mismatch")
    version = receipt["version"]
    if (
        type(version) is not dict
        or set(version) != {"argv", "returncode", "stdout", "stderr"}
        or version["argv"] != ["--version"]
        or type(version["returncode"]) is not int
        or version["returncode"] != 0
        or type(version["stdout"]) is not str
        or type(version["stderr"]) is not str
    ):
        raise PreparationError("runtime_receipt_invalid")
    if mode is not None and mode != profile["mode"]:
        raise PreparationError("runtime_profile_mismatch")
    root = path.parent
    verify_files(root, profile, metadata=True)
    manifest_path = inside_file(root, profile["runtime_library_manifest"])
    if read_json(manifest_path, "runtime_receipt_invalid") != profile["libraries"]:
        raise PreparationError("runtime_asset_mismatch")
    return profile, root / profile["engine_binary"], manifest_path
