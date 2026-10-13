"""Prepare pinned portable Prism/Qwen assets on D:, with streaming downloads and hashes."""

import argparse
import hashlib
import json
import os
import re
import time
import zipfile
from datetime import UTC, datetime
from pathlib import Path

import httpx

ROOT = Path(__file__).resolve().parents[1]
RELEASE = "prism-b10743-adfffbe"
ARCHIVE = f"llama-{RELEASE}-bin-win-cpu-x64.zip"
ENGINE_URL = f"https://github.com/PrismML-Eng/llama.cpp/releases/download/{RELEASE}/{ARCHIVE}"
ENGINE_SHA256 = "d0b3016c9cc4bc1385de68be034adee570277ba952dd94292ba3888b7f18cc44"
ENGINE_BYTES = 19_441_568
MODEL_REVISION = "a9a60d009fa7ff9606305047c2bf77ac25dbec49"
MODEL_NAME = "Qwen3-4B-Q4_K_M.gguf"
MODEL_URL = f"https://huggingface.co/Qwen/Qwen3-4B-GGUF/resolve/{MODEL_REVISION}/{MODEL_NAME}"
MODEL_SHA256 = "7485fe6f11af29433bc51cab58009521f205840f5b4ae3a32fa7f92e8534fdf5"
MODEL_BYTES = 2_497_280_256


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def require_destination(path):
    resolved = path.resolve()
    if resolved.drive.upper() != "D:" or not resolved.is_relative_to(ROOT.resolve()):
        raise ValueError("assets_must_stay_in_D_workspace")
    return resolved


def verified_download(url, path, expected_size, expected_sha256):
    path = require_destination(path)
    if path.exists():
        if path.stat().st_size != expected_size or digest(path) != expected_sha256:
            raise ValueError("existing_asset_identity_mismatch")
        print(json.dumps({"asset": path.name, "state": "already_verified"}), flush=True)
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    partial = path.with_suffix(path.suffix + ".part")
    for attempt in range(1, 4):
        offset = partial.stat().st_size if partial.exists() else 0
        if offset > expected_size:
            raise ValueError("partial_asset_exceeds_frozen_size")
        if offset == expected_size:
            break
        headers = {"Range": f"bytes={offset}-", "Accept-Encoding": "identity"} if offset else {}
        # A fresh resolver query avoids reuse of expired signed CDN redirects.
        request_url = url + ("&" if "?" in url else "?") + f"download=true&request={time.time_ns()}"
        try:
            with (
                httpx.Client(follow_redirects=True, trust_env=False, timeout=60) as client,
                client.stream("GET", request_url, headers=headers) as response,
            ):
                response.raise_for_status()
                if response.status_code == 206:
                    match = re.fullmatch(
                        r"bytes (\d+)-(\d+)/(\d+)", response.headers.get("content-range", "")
                    )
                    if (
                        match is None
                        or int(match[1]) != offset
                        or int(match[2]) != expected_size - 1
                        or int(match[3]) != expected_size
                    ):
                        raise ValueError("download_range_mismatch")
                elif response.status_code == 200:
                    offset = 0  # The server ignored Range; restart instead of appending.
                else:
                    raise ValueError("unexpected_download_status")
                advertised = response.headers.get("content-length")
                if advertised is not None and int(advertised) != expected_size - offset:
                    raise ValueError("download_size_mismatch")
                received, notified = offset, offset
                with partial.open("ab" if offset else "wb") as stream:
                    for chunk in response.iter_bytes(chunk_size=1024**2):
                        if received + len(chunk) > expected_size:
                            raise ValueError("download_exceeds_frozen_size")
                        stream.write(chunk)
                        received += len(chunk)
                        if received - notified >= 64 * 1024**2:
                            print(
                                json.dumps(
                                    {
                                        "asset": path.name,
                                        "bytes": received,
                                        "total": expected_size,
                                        "attempt": attempt,
                                    }
                                ),
                                flush=True,
                            )
                            notified = received
                    stream.flush()
                    os.fsync(stream.fileno())
                if received == expected_size:
                    break
                raise httpx.ReadError("download_incomplete")
        except httpx.HTTPError as exc:
            print(
                json.dumps({"asset": path.name, "attempt": attempt, "failure": type(exc).__name__}),
                flush=True,
            )
            if attempt == 3:
                raise RuntimeError("asset_download_failed; partial retained for resume") from None
    if partial.stat().st_size != expected_size or digest(partial) != expected_sha256:
        raise ValueError("download_hash_mismatch; partial retained for inspection")
    partial.rename(path)
    print(
        json.dumps(
            {
                "asset": path.name,
                "state": "verified",
                "bytes": expected_size,
                "sha256": expected_sha256,
            }
        ),
        flush=True,
    )


def unpack(archive, destination):
    destination = require_destination(destination)
    with zipfile.ZipFile(archive) as package:
        entries = package.infolist()
        from inferyard.config.preparation_io import PreparationError
        from inferyard.platforms.runtime_archive import zip_entries

        try:
            zip_entries(package)
        except PreparationError:
            raise ValueError("unsafe_engine_archive_entry") from None
        if not destination.exists():
            destination.mkdir(parents=True)
            package.extractall(destination)
        # Existing installations are accepted only when every packaged file matches.
        for entry in entries:
            if entry.is_dir():
                continue
            target = destination / entry.filename
            if target.is_symlink() or not target.is_file():
                raise ValueError("incomplete_or_unsafe_engine_installation")
            with package.open(entry) as stream:
                expected = hashlib.file_digest(stream, "sha256").hexdigest()
            if digest(target) != expected:
                raise ValueError("existing_engine_file_mismatch")
    servers = list(destination.rglob("llama-server.exe"))
    if len(servers) != 1:
        raise ValueError("unique_server_binary_required")
    server = servers[0]
    files = [server, *sorted(server.parent.glob("*.dll"))]
    manifest = {path.name: digest(path) for path in files}
    manifest_path = server.parent / "engine-sha256.json"
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8", newline="\n")
    return server, manifest_path, manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--engine-only", action="store_true")
    args = parser.parse_args()
    if os.name != "nt":
        raise SystemExit("native_windows_required")
    require_destination(ROOT)
    archive = ROOT / ".tools/downloads" / ARCHIVE
    verified_download(ENGINE_URL, archive, ENGINE_BYTES, ENGINE_SHA256)
    server, manifest_path, libraries = unpack(archive, ROOT / "artifacts/windows/prism-b10743")
    receipt = {
        "kind": "windows_runtime_assets.v1",
        "engine_release": RELEASE,
        "engine_source_url": ENGINE_URL,
        "engine_archive_sha256": ENGINE_SHA256,
        "engine_binary": str(server),
        "engine_sha256": libraries[server.name],
        "runtime_library_manifest": str(manifest_path),
        "libraries": libraries,
        "model_verified": False,
    }
    if not args.engine_only:
        model = ROOT / "artifacts/qwen3-4b" / MODEL_NAME
        verified_download(MODEL_URL, model, MODEL_BYTES, MODEL_SHA256)
        receipt.update(
            model_verified=True,
            model_source_url=MODEL_URL,
            model_revision=MODEL_REVISION,
            model_path=str(model),
            model_sha256=MODEL_SHA256,
            model_bytes=MODEL_BYTES,
        )
    receipt_path = ROOT / ".tools/downloads/windows-runtime.json"
    receipt_path.write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8", newline="\n")
    from collect_windows_machine import collect_machine

    machine = collect_machine(
        ROOT / ".tools/machine" / datetime.now(UTC).strftime("runtime-%Y%m%dT%H%M%S%fZ")
    )
    print(
        json.dumps(
            {
                "receipt": str(receipt_path),
                "model_verified": receipt["model_verified"],
                "machine_preparation": machine,
            }
        ),
        flush=True,
    )


if __name__ == "__main__":
    main()
