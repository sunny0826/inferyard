"""Copied beside installed test inputs; imports only the installed package, never the checkout."""

import argparse
import hashlib
import json
import subprocess
import sys
from pathlib import Path

from inferyard import __version__
from inferyard.analysis.scoring import scorer_hash
from inferyard.application.types import VerificationOptions
from inferyard.provenance import tool_source_hash
from inferyard.reporting.report import verify_report, write_report
from inferyard.reporting.report_assets import template_hash


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected", type=Path, required=True)
    parser.add_argument("--fixtures", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    import inferyard

    root = Path(inferyard.__file__).parent
    expected = json.loads(args.expected.read_text(encoding="utf-8"))
    resources = {
        p.relative_to(root).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
        for p in root.rglob("*")
        if p.is_file() and "__pycache__" not in p.parts
    }
    assert resources == expected["resources"], "installed resource bytes differ"
    assert tool_source_hash() == expected["tool_source_hash"], "installed tool identity differs"
    assert scorer_hash() == expected["scorer_hash"], "installed scoring identity differs"
    assert __version__ == "0.0.1"
    args.out.mkdir()
    run = args.fixtures / expected["fixture_relative"]
    current = args.out / "current-report"
    write_report([run], current)
    assert verify_report(current)["verified"]
    assert verify_report(current, options=VerificationOptions(rerender=True))["verified"]
    rejected = []
    for version in range(1, 8):
        out = args.out / f"unsupported-report-v{version}"
        out.mkdir()
        (out / "index.json").write_text(
            json.dumps({"schema_version": 3, "report_format_version": version}), encoding="utf-8"
        )
        result = subprocess.run(
            [sys.executable, "-I", "-m", "inferyard", "verify", "--path", str(out)],
            capture_output=True,
            text=True,
            encoding="utf-8",
            check=False,
        )
        assert result.returncode == 2, (version, result.returncode)
        assert "unsupported_format" in json.loads(result.stdout)["limitations"]
        rejected.append(version)
    templates = {"8": template_hash(8)}
    print(
        json.dumps(
            {
                "version": __version__,
                "installed_root": str(root),
                "tool_source_hash": tool_source_hash(),
                "scorer_hash": scorer_hash(),
                "resource_files": len(resources),
                "current_report_formats_verified": [8],
                "unsupported_report_formats_rejected": rejected,
                "template_hashes": templates,
            }
        )
    )


if __name__ == "__main__":
    main()
