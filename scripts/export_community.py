"""Export reviewed corpora and pinned archive-derived profiles; never download or execute assets."""

import argparse
import hashlib
import json
import zipfile
from pathlib import Path

from inferyard.config.bundle import require_review
from inferyard.config.community_resources import BUNDLES
from inferyard.config.preparation_io import json_bytes, sha256
from inferyard.contracts.validation import strict_json_loads
from inferyard.platforms.runtime_archive import member_records
from inferyard.platforms.runtime_profiles import (
    PROFILES,
    output_files,
    profile_header,
    validate_profile,
)

ROOT = Path(__file__).resolve().parents[1]
TARGET = ROOT / "src/inferyard/data/community"


def generated(archives):
    outputs = {}
    for name in BUNDLES:
        raw = (ROOT / f"bundles/{name}.json").read_bytes()
        require_review(strict_json_loads(raw.decode()))
        outputs[f"bundles/{name}.json"] = raw
    for name in PROFILES:
        profile = profile_header(name)
        if archives is None:
            # No archive trust is derived here: only revalidate already generated package data.
            raw = (TARGET / f"profile/{name}.json").read_bytes()
            validate_profile(strict_json_loads(raw.decode()), name)
        else:
            members = []
            for asset in profile["archives"]:
                path = archives / asset["name"]
                with path.open("rb") as stream:
                    digest = hashlib.file_digest(stream, "sha256").hexdigest()
                if path.stat().st_size != asset["bytes"] or digest != asset["sha256"]:
                    raise ValueError("archive_identity_mismatch")
                with zipfile.ZipFile(path) as package:
                    members.extend(member_records(package, asset["role"]))
            profile["members"] = sorted(members, key=lambda m: (m["role"], m["path"]))
            server, manifest, libraries, _ = output_files(profile["members"])
            profile.update(
                engine_binary=server, runtime_library_manifest=manifest, libraries=libraries
            )
            validate_profile(profile, name)
            raw = json_bytes(profile)
        outputs[f"profile/{name}.json"] = raw
    for path in [TARGET / "README.md", *sorted((TARGET / "configs").glob("*.toml"))]:
        outputs[path.relative_to(TARGET).as_posix()] = path.read_bytes()
    if {name for name in outputs if name.startswith("configs/")} != {
        f"configs/{s}.example.toml" for s in ("linux", "macos", "windows")
    }:
        raise ValueError("three_platform_templates_required")
    outputs["resources.json"] = json_bytes(
        {name: sha256(raw) for name, raw in sorted(outputs.items())}
    )
    return outputs


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archives", type=Path, help="directory containing all three fixed ZIPs")
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    if not args.check and args.archives is None:
        parser.error(
            "generation requires --archives; --check without archives checks committed resources"
        )
    outputs = generated(args.archives)
    if args.check:
        for name, raw in outputs.items():
            if (TARGET / name).read_bytes() != raw:
                raise SystemExit(f"community resource drift: {name}")
        actual = {p.relative_to(TARGET).as_posix() for p in TARGET.rglob("*") if p.is_file()}
        if actual != set(outputs):
            raise SystemExit("unexpected community resources")
    else:
        for name, raw in outputs.items():
            path = TARGET / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(raw)
    print(
        json.dumps(
            {
                "checked": args.check,
                "resources": len(outputs),
                "archives_verified": args.archives is not None,
            }
        )
    )


if __name__ == "__main__":
    main()
