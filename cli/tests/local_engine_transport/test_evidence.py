import asyncio
from copy import deepcopy
from types import SimpleNamespace

import pytest

from inferyard.adapters.local_engines.protocol import FAMILIES, mlx_sources
from inferyard.evidence.local_engines import verify_queue_events
from inferyard.evidence.storage import EvidenceError


@pytest.fixture(autouse=True)
def queue_only_identity_boundary(monkeypatch):
    """Queue-only tests; runtime integration exercises full frozen identity validation."""
    monkeypatch.setattr(
        "inferyard.evidence.local_engines.validate_frozen_identity", lambda *args, **kwargs: None
    )


class Reads:
    def __init__(self, r, props):
        roots = [
            {
                "path": "/installed/inferyard",
                "entries": [
                    {"path": path, "sha256": mlx_sources()[key]}
                    for key, path in (
                        ("local_engine_source_sha256", "data/local_engine/mlx_server.py"),
                        ("generation_queue_source_sha256", "data/engine_fit/mlx_server.py"),
                    )
                ],
            }
        ]
        self.values = {
            "identity": {
                "files": [],
                "local_engine": {
                    "runtime": {"roots": roots},
                    "binding": {
                        "schema_version": 3,
                        "definition": "local-engine-binding.v1",
                        "installation_sha256": r.config["engine"]["installation_sha256"],
                        "service": deepcopy(r.binding),
                    },
                },
            },
            "engine_capabilities": deepcopy(r.adapter.capability_evidence),
            "service_properties": deepcopy(props),
        }
        self.receipts = ["req"]

    def observation(self, kind):
        return self.values[kind]

    def observation_keys(self, kind, stage=None):
        assert kind == "native_clean_receipt"
        return self.receipts


def event(kind, data, key=None):
    return {"event_type": kind, "request_id": key, "data": data}


def capture(rig, family="vllm"):
    async def run():
        r = rig(family)
        r.binding["workers"] = []
        a = r.adapter
        events = []
        try:
            props = await a.verify_properties(r.config)
            assert await a.wait_ready(
                observe=lambda _, data: events.append(event("native_observed", data))
            )
            events.append(event("request_started", {}, "req"))
            terminal = await a.generate(a.request_body(r.config, "hello"), 1)
            events.append(event("request_finished", terminal, "req"))
            assert await a.wait_ready(
                observe=lambda _, data: events.append(event("native_observed", data, "req"))
            )
            events.append(event("run_stopped", {"reason": "plan_finished"}))
            return SimpleNamespace(config=r.config, events=events, reads=Reads(r, props))
        finally:
            await a.close()

    return asyncio.run(run())


def snapshot(evidence, index):
    return evidence.events[index]["data"]["signals"]["queue_snapshot"]


def verify(e):
    return verify_queue_events("/unused", e.config, e.events, reads=e.reads)


def metrics(e, index, value):
    item = snapshot(e, index)
    item["raw_metrics"] = value
    e.events[index]["data"]["signals"]["/metrics"]["body"] = value


def test_valid_all_families_without_live_binding(rig):
    for family in FAMILIES:
        e = capture(rig, family)
        assert verify(e) is False


@pytest.mark.parametrize(
    "change",
    [
        lambda e: snapshot(e, 0)["binding"].update(pid=999),
        lambda e: snapshot(e, 0).update(source="guessed"),
        lambda e: snapshot(e, 0).update(family="llama-cpp"),
        lambda e: snapshot(e, 0)["observation"].update(idle=False),
        lambda e: metrics(e, 0, snapshot(e, 0)["raw_metrics"].replace(" 0", " 1", 1)),
        lambda e: snapshot(e, 0).update(raw_metrics="vllm:num_requests_running NaN\n"),
        lambda e: e.events[0]["data"].update(engine_internal_drain=True),
        lambda e: e.events.pop(0),
        lambda e: e.events.pop(3),
        lambda e: e.reads.receipts.clear(),
        lambda e: e.reads.values["identity"]["local_engine"]["binding"]["service"].update(
            start_ticks=999
        ),
        lambda e: e.reads.values["engine_capabilities"].update(family="llama-cpp"),
    ],
)
def test_tampered_queue_evidence_is_rejected(rig, change):
    e = capture(rig)
    change(e)
    with pytest.raises(EvidenceError):
        verify(e)


@pytest.mark.parametrize("terminal", ["failed", "cancelled"])
def test_uncertain_cannot_claim_clean_or_admit_again(rig, terminal):
    e = capture(rig)
    e.events[2]["data"]["execution_state"] = terminal
    with pytest.raises(EvidenceError, match="uncertainty_erased"):
        verify(e)
    snapshot(e, 3)["client_uncertain"] = True
    e.events[-1]["data"]["reason"] = "interrupted"
    e.reads.receipts.clear()
    assert verify(e) is False
    e.events.insert(-1, event("native_observed", deepcopy(e.events[0]["data"])))
    with pytest.raises(EvidenceError, match="uncertainty_erased"):
        verify(e)


def test_post_observation_failure_retains_valid_incomplete_original(rig):
    e = capture(rig)
    snapshot(e, 3).update(
        observation=None, missing_reason="local_engine_queue_unverified", client_uncertain=True
    )
    e.events[-1]["data"]["reason"] = "service_stop_unconfirmed"
    e.reads.receipts.clear()
    assert verify(e) is False
    e.reads.receipts.append("req")
    with pytest.raises(EvidenceError, match="receipt_mismatch"):
        verify(e)


def test_mlx_offline_source_uses_sealed_runtime_not_current_installation(rig, monkeypatch):
    e = capture(rig, "mlx-lm")
    monkeypatch.setattr(
        "inferyard.adapters.local_engines.protocol.mlx_sources", lambda: {"unexpected": "hash"}
    )
    assert verify(e) is False
    e.reads.values["identity"]["local_engine"]["runtime"]["roots"][0]["entries"][0]["sha256"] = (
        "0" * 64
    )
    with pytest.raises(EvidenceError):
        verify(e)


def test_lmstudio_normalized_counts_are_recomputed(rig):
    e = capture(rig, "lmstudio")
    observation = snapshot(e, 0)["observation"]
    observation["values"][0]["value"] = 1
    with pytest.raises(EvidenceError):
        verify(e)
