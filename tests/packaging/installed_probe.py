"""Copied beside installed test inputs; imports only the installed package, never the checkout."""

import argparse
import hashlib
import json
from pathlib import Path

from inferyard import __version__
from inferyard.analysis.scoring import scorer_hash
from inferyard.provenance import tool_source_hash
from inferyard.reporting.report import _environment, build_index, verify_report
from inferyard.reporting.report_assets import template_hash, template_name
from inferyard.reporting.report_common import render_report_html


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
    verified = []
    run = args.fixtures / expected["fixture_relative"]
    # These are newly rendered synthetic historical-format inputs, not historical device evidence.
    for version in range(1, 7):
        out = args.out / f"synthetic-report-v{version}"
        out.mkdir()
        index = build_index([run], out, format_version=version, producer=tool_source_hash())
        (out / "index.json").write_text(
            json.dumps(index, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
        )
        (out / "report.html").write_text(
            render_report_html(_environment(), template_name(version), index), encoding="utf-8"
        )
        assert verify_report(out)["verified"]
        verified.append(version)
    templates = {str(version): template_hash(version) for version in range(1, 8)}
    print(
        json.dumps(
            {
                "version": __version__,
                "installed_root": str(root),
                "tool_source_hash": tool_source_hash(),
                "scorer_hash": scorer_hash(),
                "resource_files": len(resources),
                "synthetic_historical_reports_verified": verified,
                "template_hashes": templates,
            }
        )
    )


if __name__ == "__main__":
    main()
