"""Shared engine template environment; presentation HTML is retired."""

import base64
import hashlib
import json

from jinja2 import Environment, PackageLoader, StrictUndefined
from markupsafe import Markup, escape

from inferyard.reporting.svg_render import animation_stats


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
        content_text=lambda value: escape(value).replace("\r", Markup("&#13;")),
        pretty=lambda value: json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False),
        percent=lambda value: "—" if value is None else f"{value * 100:.2f}%",
        seconds=lambda value: "—" if value is None else f"{value / 1e9:.4f} 秒",
        gib=lambda value: "—" if value is None else f"{value / 1024**3:.3f} GiB",
        memory=lambda value: (
            "—" if value is None else f"{value / 1024**3:.2f}".rstrip("0").rstrip(".") + " GiB"
        ),
        decimal=lambda value: "—" if value is None else f"{value:+.2f}",
        milliseconds=lambda value: "—" if value is None else f"{value / 1000:.2f}",
        svg_animation_stats=animation_stats,
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
