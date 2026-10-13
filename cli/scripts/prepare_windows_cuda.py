"""Prepare the pinned Prism Windows CUDA 12.4 runtime beside existing CPU assets."""

import json
import os
import shutil
import subprocess
import zipfile

from prepare_windows_runtime import (
    RELEASE,
    ROOT,
    digest,
    require_destination,
    unpack,
    verified_download,
)

ASSETS = (
    (
        f"llama-{RELEASE}-bin-win-cuda-12.4-x64.zip",
        257322810,
        "1b849f713bee42fda258de83770cd422e8f48dd631ce370eb0641f6458c69d87",
    ),
    (
        "cudart-llama-bin-win-cuda-12.4-x64.zip",
        391443627,
        "8c79a9b226de4b3cacfd1f83d24f962d0773be79f1e7b75c6af4ded7e32ae1d6",
    ),
)


def main():
    if os.name != "nt":
        raise ValueError("native_windows_required")
    receipts = []
    for name, size, sha256 in ASSETS:
        url = f"https://github.com/PrismML-Eng/llama.cpp/releases/download/{RELEASE}/{name}"
        archive = ROOT / ".tools/downloads" / name
        verified_download(url, archive, size, sha256)
        receipts.append(dict(name=name, size=size, sha256=sha256, url=url))
    destination = ROOT / "artifacts/windows/prism-b10743-cuda12.4"
    server, manifest, _ = unpack(ROOT / ".tools/downloads" / ASSETS[0][0], destination)
    # Runtime ZIPs contain DLLs without a server executable. Validate every path
    # before extraction; preserve previously verified files rather than overwrite.
    runtime = require_destination(ROOT / ".tools/cuda12.4-runtime")
    with zipfile.ZipFile(ROOT / ".tools/downloads" / ASSETS[1][0]) as package:
        entries = package.infolist()
        from inferyard.config.preparation_io import PreparationError
        from inferyard.platforms.runtime_archive import zip_entries

        try:
            zip_entries(package)
        except PreparationError:
            raise ValueError("unsafe_cuda_runtime_entry") from None
        runtime.mkdir(parents=True, exist_ok=True)
        for entry in entries:
            if entry.is_dir():
                continue
            target = runtime / entry.filename
            target.parent.mkdir(parents=True, exist_ok=True)
            with package.open(entry) as source:
                import hashlib

                expected = hashlib.file_digest(source, "sha256").hexdigest()
            if not target.exists():
                with package.open(entry) as source, target.open("xb") as output:
                    shutil.copyfileobj(source, output)
            if target.is_symlink() or digest(target) != expected:
                raise ValueError("cuda_runtime_identity_mismatch")
            if target.suffix.lower() == ".dll":
                final = server.parent / target.name
                if final.exists():
                    if digest(final) != expected:
                        raise ValueError("cuda_library_collision")
                else:
                    shutil.copyfile(target, final)
    libraries = {p.name: digest(p) for p in [server, *sorted(server.parent.glob("*.dll"))]}
    manifest.write_text(json.dumps(libraries, indent=2) + "\n", encoding="utf-8")
    version = subprocess.run(
        [str(server), "--version"],
        capture_output=True,
        text=True,
        check=True,
        creationflags=subprocess.CREATE_NO_WINDOW,
    )
    receipt = dict(
        kind="windows_cuda_runtime_assets.v1",
        release=RELEASE,
        assets=receipts,
        binary_path=str(server),
        binary_sha256=libraries[server.name],
        runtime_library_manifest=str(manifest),
        libraries=libraries,
        version_stdout=version.stdout,
        version_stderr=version.stderr,
    )
    receipt_path = ROOT / ".tools/downloads/windows-cuda-runtime.json"
    receipt_path.write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(dict(receipt=str(receipt_path), engine=str(server))), flush=True)


if __name__ == "__main__":
    main()
