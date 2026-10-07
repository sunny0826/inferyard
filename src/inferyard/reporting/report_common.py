"""Shared offline report rendering and protected output publication."""

import base64
import hashlib
import json
import os
from pathlib import Path
from urllib.parse import quote

from jinja2 import Environment, PackageLoader, StrictUndefined

from inferyard.evidence.storage import EvidenceError


def evidence_url(source: Path, out: Path) -> str:
    """Local links may cross Windows drives; such paths have no relative form."""
    try:
        return quote(os.path.relpath(source, out), safe="/")
    except ValueError:
        return source.resolve().as_uri()


def _environment():
    environment = Environment(
        loader=PackageLoader("inferyard", "templates"),
        autoescape=True,
        undefined=StrictUndefined,
    )
    # Resolve every frozen snapshot include against the same snapshot, preserving original bytes.
    environment.join_path = lambda template, parent: (
        parent.rsplit("/", 1)[0] + "/" + template if "/" in parent else template
    )
    # Default CSP hash for direct renders of current templates; render_report_html overrides
    # per template version so frozen snapshots stay byte-stable across script edits.
    script = environment.get_template("report_script.html").render()
    environment.globals["report_script_sha256"] = base64.b64encode(
        hashlib.sha256(script.encode()).digest()
    ).decode("ascii")
    environment.filters.update(
        pretty=lambda value: json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False),
        percent=lambda value: "—" if value is None else f"{value * 100:.2f}%",
        seconds=lambda value: "—" if value is None else f"{value / 1e9:.4f} 秒",
        gib=lambda value: "—" if value is None else f"{value / 1024**3:.3f} GiB",
        memory=lambda value: (
            "—" if value is None else f"{value / 1024**3:.2f}".rstrip("0").rstrip(".") + " GiB"
        ),
        decimal=lambda value: "—" if value is None else f"{value:+.2f}",
        milliseconds=lambda value: "—" if value is None else f"{value / 1000:.2f}",
        display=lambda value: (
            "—"
            if value is None
            else "是"
            if value is True
            else "否"
            if value is False
            else json.dumps(value, ensure_ascii=False)
            if isinstance(value, (dict, list))
            else str(value)
        ),
    )
    return environment


def render_report_html(environment, name, index):
    """Render a report with the CSP hash of the script from the same template version."""
    prefix = name.rsplit("/", 1)[0] + "/" if "/" in name else ""
    script = environment.get_template(prefix + "report_script.html").render()
    digest = base64.b64encode(hashlib.sha256(script.encode()).digest()).decode("ascii")
    return environment.get_template(name).render(index=index, report_script_sha256=digest)


def _new_output(out: Path, inputs: list[Path]):
    resolved = out.resolve()
    if any(resolved.is_relative_to(path.resolve()) for path in inputs):
        raise EvidenceError("report_output_inside_original_evidence")
    out.parent.mkdir(parents=True, exist_ok=True)
    out.mkdir(mode=0o700)
