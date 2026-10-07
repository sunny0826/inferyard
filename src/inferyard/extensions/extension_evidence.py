"""Separate sealed extension packets and offline reports; never forge serial trials."""

import html
import os
import time
from datetime import UTC, datetime

from inferyard import SCHEMA_VERSION
from inferyard.contracts.schemas import IDENTIFIER, LABEL, NAT, POS, obj
from inferyard.contracts.validation import _validate, validate_document
from inferyard.evidence.storage import (
    EvidenceError,
    EvidenceStore,
    fsync_directory,
    json_bytes,
    read_json,
    verify_manifest,
)
from inferyard.extensions.closed_concurrency import reduce_closed
from inferyard.extensions.native_tools import reduce_tools
from inferyard.extensions.total_observer_control import assess_total
from inferyard.platforms.platform_io import open_nofollow
from inferyard.provenance import tool_source_hash


class ExtensionJournal(EvidenceStore):
    def event(self, event_type, phase, request_id, data, *, monotonic_ns=None):
        seq = self._seq["events.jsonl"] + 1
        value = {
            "definition": "extension_event.v1",
            "run_id": self.run_id,
            "seq": seq,
            "clock_id": self.clock_id,
            "phase": phase,
            "event_type": event_type,
            "request_id": request_id,
            "data": data,
            "monotonic_ns": time.monotonic_ns() if monotonic_ns is None else monotonic_ns,
            "utc": datetime.now(UTC).isoformat(),
        }
        spec = obj(
            {
                "definition": {"type": "string", "const": "extension_event.v1"},
                "run_id": IDENTIFIER,
                "seq": POS,
                "clock_id": LABEL,
                "phase": LABEL,
                "event_type": LABEL,
                "request_id": {"type": ["string", "null"]},
                "data": {"type": "object"},
                "monotonic_ns": NAT,
                "utc": LABEL,
            }
        )
        _validate(value, spec, "extension_event")
        self._append(
            "events.jsonl",
            value,
            sync=event_type
            in ("request_started", "request_finished", "tool_terminal", "control_checkpoint"),
        )
        self._seq["events.jsonl"] = seq
        return value

    def observation(self, name, value):
        if name in ("external-cpu.jsonl", "request-environment.jsonl", "safety-checks.jsonl"):
            if self.sealed:
                raise EvidenceError("sealed_run")
            if name not in self._logs:
                fd = open_nofollow(self.path / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL)
                self._logs[name] = os.fdopen(fd, "wb")
                self._pending_sync.add(name)
                fsync_directory(self.path)
            self._append(name, value)
        else:
            super().observation(name, value)

    def sample(self, sample):
        seq = self._seq["memory.jsonl"] + 1
        value = {
            **sample,
            "schema_version": SCHEMA_VERSION,
            "run_id": self.run_id,
            "experiment_id": "extension",
            "trial_id": self.run_id,
            "seq": seq,
            "clock_id": self.clock_id,
            "utc": datetime.now(UTC).isoformat(),
        }
        validate_document("sample", value)
        self._append("memory.jsonl", value)
        self._seq["memory.jsonl"] = seq


def reduce_packet(packet):
    if (
        not isinstance(packet, dict)
        or set(packet) - {"spec", "rows", "evidence_kind", "guard", "client_capacity"}
        or not {"spec", "rows", "evidence_kind"} <= packet.keys()
    ):
        raise EvidenceError("extension_packet_fields_invalid")
    spec = packet["spec"]
    if spec.get("definition") == "conditional_events.v1":
        from inferyard.evidence.conditional_events import reduce_import

        return reduce_import(spec, packet["rows"], evidence_kind=packet["evidence_kind"])
    if spec.get("definition") == "closed_concurrency.v1":
        return reduce_closed(
            spec,
            packet["rows"],
            evidence_kind=packet["evidence_kind"],
            client_capacity=packet.get("client_capacity"),
        )
    if spec.get("definition") == "native_tools.v1":
        return reduce_tools(spec, packet["rows"], evidence_kind=packet["evidence_kind"])
    if spec.get("definition") in ("total_observer_control.v1", "total_observer_control.v2"):
        return assess_total(
            spec, packet["rows"], packet.get("guard", {}), evidence_kind=packet["evidence_kind"]
        )
    raise EvidenceError("extension_definition_not_supported")


def render(summary):
    text = html.escape(json_bytes(summary).decode())
    return (
        '<!doctype html><html lang="zh-CN"><meta charset="utf-8">'
        '<meta name="viewport" content="width=device-width,initial-scale=1">'
        "<title>扩展协议核验</title><style>body{max-width:900px;margin:40px auto;"
        "padding:16px;font:18px/1.6 system-ui;background:#f3f6ef}"
        "pre{white-space:pre-wrap;overflow-wrap:anywhere;font-size:14px}</style>"
        "<h1>扩展协议核验</h1><p>Fixture代表软件验证；真机资格按独立证据标记。</p><pre>"
        + text
        + "</pre></html>"
    )


def save_packet(out, packet, *, origin="offline_fixture"):
    summary = reduce_packet(packet)
    with_store = ExtensionJournal(out)
    try:
        with_store.snapshot(
            "run.json",
            {
                "kind": "extension_packet.v1",
                "origin": origin,
                "tool_source_sha256": tool_source_hash(),
                "evidence_kind": packet["evidence_kind"],
            },
        )
        with_store.snapshot("packet.json", packet)
        with_store.snapshot("summary.json", summary)
        with_store.text_snapshot("report.html", render(summary))
        with_store.seal()
    finally:
        with_store.close()
    return with_store.path, summary


def verify_packet(path, *, verified_trials=None):
    if verify_manifest(path):
        raise EvidenceError("extension_packet_unsealed_or_derived_damaged")
    run = read_json(path / "run.json")
    packet = read_json(path / "packet.json")
    if (
        run.get("kind") != "extension_packet.v1"
        or run.get("evidence_kind") != packet["evidence_kind"]
    ):
        raise EvidenceError("extension_packet_origin_mismatch")
    if packet["evidence_kind"] == "live":
        if run.get("origin") != "live_driver":
            raise EvidenceError("extension_live_origin_unverified")
        plan = read_json(path / "plan.json")
        if plan.get("spec") != packet["spec"] or plan.get("tool_source_sha256") != run.get(
            "tool_source_sha256"
        ):
            raise EvidenceError("extension_live_plan_binding_mismatch")
        if read_json(path / "identity.json").get("verification") != "verified":
            raise EvidenceError("extension_live_identity_unverified")
        for child in read_json(path / "child-evidence.json"):
            from inferyard.evidence.storage import local_file, sha256_file

            root = local_file(path, child["path"])
            if sha256_file(root / "manifest.json") != child["manifest_sha256"] or verify_manifest(
                root
            ):
                raise EvidenceError("extension_child_observer_evidence_changed")
        if packet["spec"]["definition"] == "total_observer_control.v2":
            from inferyard.extensions.trial_control_evidence import verify_control_rows

            verify_control_rows(path, packet, verified_trials=verified_trials)
    summary = reduce_packet(packet)
    if summary != read_json(path / "summary.json") or (path / "report.html").read_text() != render(
        summary
    ):
        raise EvidenceError("extension_packet_does_not_replay")
    return summary
