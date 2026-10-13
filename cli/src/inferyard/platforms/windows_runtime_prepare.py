"""Prepare only caller-supplied pinned Windows archives; never download or start a service."""

import zipfile
from contextlib import ExitStack
from pathlib import PurePosixPath

from inferyard.config.preparation_io import (
    NewDirectory,
    PreparationError,
    base_details,
    file_record,
    new_path,
    require_platform,
)
from inferyard.platforms.runtime_archive import zip_entries
from inferyard.platforms.runtime_profiles import load_profile, output_files
from inferyard.platforms.runtime_verification import query_version, verify_files


def prepare(request):
    require_platform(("Windows", "x64"))
    new_path(request.out, (request.archive, request.runtime_archive))
    profile, digest = load_profile(request.runtime_profile)
    if profile["mode"] == "cuda" and request.runtime_archive is None:
        raise PreparationError("runtime_archive_required")
    if profile["mode"] == "cpu" and request.runtime_archive is not None:
        raise PreparationError("runtime_archive_forbidden")
    sources = {"engine": request.archive, "runtime": request.runtime_archive}
    with ExitStack() as stack:
        packages = {}
        for archive in profile["archives"]:
            source = sources[archive["role"]]
            record = file_record(source)
            if (record["bytes"], record["sha256"]) != (archive["bytes"], archive["sha256"]):
                raise PreparationError("archive_identity_mismatch")
            try:
                package = stack.enter_context(zipfile.ZipFile(source))
                entries = zip_entries(package)
            except zipfile.BadZipFile:
                raise PreparationError("unsafe_archive_entry") from None
            expected = {m["path"]: m for m in profile["members"] if m["role"] == archive["role"]}
            if set(entries) != set(expected) or any(
                entries[n].file_size != m["bytes"] for n, m in expected.items()
            ):
                raise PreparationError("runtime_asset_mismatch")
            packages[archive["role"]] = package
        output = NewDirectory(request.out, tuple(sources.values()))
        _, manifest, libraries, final_files = output_files(profile["members"])
        for relative, member in sorted(final_files.items()):
            package = packages[member["role"]]
            with package.open(member["path"]) as stream:
                output.copy(relative, stream, member)
    verify_files(output.path, profile)
    output.json(manifest, libraries)
    engine = output.path / profile["engine_binary"]
    version = query_version(
        engine, profile["version_pattern"], timeout=profile["version_timeout_seconds"]
    )
    verify_files(output.path, profile, metadata=True)
    receipt = {
        "kind": "community_runtime_receipt.v1",
        "schema_version": 3,
        "profile_sha256": digest,
        "engine_sha256": libraries[PurePosixPath(profile["engine_binary"]).name],
        "version": version,
        "model_requests_sent": 0,
        "ready_to_run": False,
        **{
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
        },
    }
    output.json("runtime-receipt.json", receipt)
    return {
        **base_details(output.path),
        "profile": profile["profile"],
        "mode": profile["mode"],
        "engine": str(engine),
        "engine_sha256": receipt["engine_sha256"],
        "runtime_library_manifest": str(output.path / manifest),
        "receipt": str(output.path / "runtime-receipt.json"),
    }
