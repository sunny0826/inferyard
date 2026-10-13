"""Pinned archive trust roots and strict installed profile validation; no downloads."""

import re
from pathlib import PurePosixPath

from inferyard.config.community_resources import resource_bytes
from inferyard.config.preparation_io import PreparationError, sha256
from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.platforms.runtime_archive import safe_member

RELEASE = "prism-b10743-adfffbe"
BASE_URL = f"https://github.com/PrismML-Eng/llama.cpp/releases/download/{RELEASE}/"
VERSION_PATTERN = r"\b10743\b.*\badfffbe(?:41)?\b"
ARCHIVES = {
    "cpu": (
        f"llama-{RELEASE}-bin-win-cpu-x64.zip",
        19441568,
        "d0b3016c9cc4bc1385de68be034adee570277ba952dd94292ba3888b7f18cc44",
    ),
    "cuda": (
        f"llama-{RELEASE}-bin-win-cuda-12.4-x64.zip",
        257322810,
        "1b849f713bee42fda258de83770cd422e8f48dd631ce370eb0641f6458c69d87",
    ),
    "runtime": (
        "cudart-llama-bin-win-cuda-12.4-x64.zip",
        391443627,
        "8c79a9b226de4b3cacfd1f83d24f962d0773be79f1e7b75c6af4ded7e32ae1d6",
    ),
}
PROFILES = {f"{RELEASE}-win-cpu-x64": "cpu", f"{RELEASE}-win-cuda-12.4-x64": "cuda"}


def profile_header(name):
    if name not in PROFILES:
        raise PreparationError("unsupported_runtime_profile")
    mode = PROFILES[name]
    archives = []
    for key, role in [(mode, "engine"), *([("runtime", "runtime")] if mode == "cuda" else [])]:
        filename, size, digest = ARCHIVES[key]
        archives.append(
            dict(
                role=role, name=filename, source_url=BASE_URL + filename, bytes=size, sha256=digest
            )
        )
    return dict(
        kind="community_runtime_profile.v1",
        schema_version=3,
        profile=name,
        platform="Windows",
        architecture="x64",
        mode=mode,
        engine_release=RELEASE,
        archives=archives,
        version_pattern=VERSION_PATTERN,
        version_timeout_seconds=10,
    )


def digest_string(value):
    return type(value) is str and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def output_files(members):
    """Derive the final tree from trusted archive members, including flattened CUDA DLL copies."""
    servers = [
        m
        for m in members
        if m["role"] == "engine" and PurePosixPath(m["path"]).name == "llama-server.exe"
    ]
    if len(servers) != 1:
        raise PreparationError("runtime_asset_mismatch")
    server = "engine/" + servers[0]["path"]
    parent = PurePosixPath(server).parent
    result, folded = {}, {}
    for member in members:
        destination = member["role"] + "/" + member["path"]
        targets = [destination]
        if member["role"] == "runtime" and PurePosixPath(member["path"]).suffix.lower() == ".dll":
            targets.append(str(parent / PurePosixPath(member["path"]).name))
        for target in targets:
            old = folded.setdefault(target.casefold(), target)
            if old != target or (
                target in result
                and (result[target]["sha256"], result[target]["bytes"])
                != (member["sha256"], member["bytes"])
            ):
                raise PreparationError("runtime_library_collision")
            result[target] = member
    libraries = {
        PurePosixPath(path).name: member["sha256"]
        for path, member in sorted(result.items())
        if PurePosixPath(path).parent == parent
        and (path == server or path.lower().endswith(".dll"))
    }
    return server, str(parent / "engine-sha256.json"), libraries, result


def exact_value(value, expected):
    """JSON equality with recursive exact types and object keys."""
    if type(value) is not type(expected):
        return False
    if type(expected) is dict:
        return set(value) == set(expected) and all(
            exact_value(value[key], item) for key, item in expected.items()
        )
    if type(expected) is list:
        return len(value) == len(expected) and all(
            exact_value(left, right) for left, right in zip(value, expected, strict=True)
        )
    return value == expected


def validate_profile(profile, name):
    header = profile_header(name)
    if type(profile) is not dict or set(profile) != set(header) | {
        "members",
        "engine_binary",
        "runtime_library_manifest",
        "libraries",
    }:
        raise ValueError
    for key, expected in header.items():
        if not exact_value(profile[key], expected):
            raise ValueError
    members = profile["members"]
    if type(members) is not list or not members:
        raise ValueError
    roles = {a["role"] for a in header["archives"]}
    seen = set()
    for member in members:
        if type(member) is not dict or set(member) != {"role", "path", "bytes", "sha256"}:
            raise ValueError
        if type(member["path"]) is not str or member["role"] not in roles:
            raise ValueError
        safe_member(member["path"])
        if (
            type(member["bytes"]) is not int
            or member["bytes"] < 0
            or not digest_string(member["sha256"])
        ):
            raise ValueError
        identity = member["role"], member["path"].casefold()
        if identity in seen:
            raise ValueError
        seen.add(identity)
    if {m["role"] for m in members} != roles:
        raise ValueError
    server, manifest, libraries, _ = output_files(members)
    if (profile["engine_binary"], profile["runtime_library_manifest"], profile["libraries"]) != (
        server,
        manifest,
        libraries,
    ):
        raise ValueError
    return profile


def load_profile(name):
    profile_header(name)
    raw = resource_bytes(f"profile/{name}.json")
    try:
        profile = validate_profile(strict_json_loads(raw.decode()), name)
    except ValueError, TypeError, KeyError, ContractError, PreparationError:
        raise PreparationError("package_resource_invalid", 4) from None
    return profile, sha256(raw)
