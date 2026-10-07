"""Verify local lineage artifacts without executing conversion commands."""

import argparse
from pathlib import Path

from inferyard.evidence.lineage_files import verify_lineage_files
from inferyard.evidence.storage import EvidenceError, atomic_bytes, json_bytes, read_json
from inferyard.provenance import tool_source_hash


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workload", required=True, type=Path)
    parser.add_argument("--bindings", required=True, type=Path)
    parser.add_argument("--root", required=True, type=Path)
    parser.add_argument("--out", required=True, type=Path)
    args = parser.parse_args()
    if args.out.exists():
        raise EvidenceError("lineage_output_exists")
    result = verify_lineage_files(read_json(args.workload), read_json(args.bindings), args.root)
    result["tool_source_sha256"] = tool_source_hash()
    atomic_bytes(args.out, json_bytes(result))
    print(json_bytes(result).decode(), end="")
    return 0 if result["all_files_verified"] else 3


if __name__ == "__main__":
    raise SystemExit(main())
