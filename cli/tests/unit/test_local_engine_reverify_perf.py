"""Regression tests for local-engine re-verification caching and safety invariants."""

from __future__ import annotations

import hashlib
from pathlib import Path

import pytest

from inferyard.contracts.validation import json_bytes
from inferyard.errors import PreflightError
from inferyard.platforms.identity import FileIdentity, hash_file
from inferyard.platforms.local_engines import mapping_permissions, service, workers


def test_permission_denied_non_exec_mapping_caches_per_process_incarnation(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    exe = tmp_path / "strata"
    exe.write_bytes(b"\xcf\xfa\xed\xfe\x0c\x00\x00\x01")
    runtime_file = hash_file(exe)

    unreadable_path = "/private/var/db/analyticsd/events.allowlist"
    user_unreadable_path = "/private/tmp/restricted-user-file.dat"
    unlinked_path = "/private/tmp/unlinked-cache.dat"
    unreadable_stamp = (16777230, 2071706)
    user_unreadable_stamp = (16777230, 8888888)
    unlinked_stamp = (16777230, 9999999)

    monkeypatch.setattr(mapping_permissions, "_NON_EXEC_CACHE", {})
    monkeypatch.setattr(workers, "verify_libraries", lambda *_a, **_kw: [])

    current_ticks = 50001
    monkeypatch.setattr(mapping_permissions, "process_start_ticks", lambda _pid: current_ticks)

    real_open = mapping_permissions.os.open

    def fake_open(path, flags, *args, **kwargs):
        p = str(path)
        if p in (unreadable_path, user_unreadable_path):
            raise PermissionError(13, "Permission denied", p)
        if p == unlinked_path:
            raise FileNotFoundError(2, "No such file", p)
        return real_open(path, flags, *args, **kwargs)

    monkeypatch.setattr(mapping_permissions.os, "open", fake_open)
    monkeypatch.setattr(workers, "mapped_native_image", lambda _p, _s: None)

    observe_calls = 0
    vmmap_mode = "r--/r--"

    def fake_observe(pid: int) -> str:
        nonlocal observe_calls
        observe_calls += 1
        return (
            f"mapped file  100000000-100004000 [ 16K  16K   0K   0K] "
            f"{vmmap_mode} SM=COW  {unreadable_path}\n"
            f"mapped file  100004000-100008000 [ 16K  16K   0K   0K] "
            f"{vmmap_mode} SM=COW  {user_unreadable_path}\n"
            f"mapped file  100008000-10000c000 [ 16K  16K   0K   0K] "
            f"{vmmap_mode} SM=COW  {unlinked_path}\n"
        )

    monkeypatch.setattr(mapping_permissions, "observe", fake_observe)

    unreadable_rows = [
        {
            "p": 777,
            "f": "txt",
            "t": "REG",
            "D": f"{unreadable_stamp[0]:x}",
            "i": str(unreadable_stamp[1]),
            "n": unreadable_path,
        }
    ]

    workers.mapped_closure(unreadable_rows, [runtime_file], exe, pid=777)
    assert observe_calls == 1

    # Repeated checks on the same (pid, start_ticks, stamp, path) skip vmmap.
    workers.mapped_closure(unreadable_rows, [runtime_file], exe, pid=777)
    workers.mapped_closure(unreadable_rows, [runtime_file], exe, pid=777)
    assert observe_calls == 1

    # New process incarnation reusing PID invalidates cache and re-runs vmmap.
    current_ticks = 50002
    workers.mapped_closure(unreadable_rows, [runtime_file], exe, pid=777)
    assert observe_calls == 2

    # Non-system PermissionError paths outside /private/var/db are never cached.
    user_unreadable_rows = [
        {
            "p": 777,
            "f": "txt",
            "t": "REG",
            "D": f"{user_unreadable_stamp[0]:x}",
            "i": str(user_unreadable_stamp[1]),
            "n": user_unreadable_path,
        }
    ]
    workers.mapped_closure(user_unreadable_rows, [runtime_file], exe, pid=777)
    workers.mapped_closure(user_unreadable_rows, [runtime_file], exe, pid=777)
    assert observe_calls == 4

    # Unlinked (FileNotFoundError) mappings are never cached across checks.
    unlinked_rows = [
        {
            "p": 777,
            "f": "txt",
            "t": "REG",
            "D": f"{unlinked_stamp[0]:x}",
            "i": str(unlinked_stamp[1]),
            "n": unlinked_path,
        }
    ]
    workers.mapped_closure(unlinked_rows, [runtime_file], exe, pid=777)
    workers.mapped_closure(unlinked_rows, [runtime_file], exe, pid=777)
    assert observe_calls == 6

    # Executable mappings on uncached paths are rejected even within the same start_ticks.
    vmmap_mode = "r-x/r-x"
    with pytest.raises(PreflightError, match="local_engine_mapping_unverified"):
        workers.mapped_closure(user_unreadable_rows, [runtime_file], exe, pid=777)
    current_ticks = 50003
    with pytest.raises(PreflightError, match="local_engine_mapping_unverified"):
        workers.mapped_closure(unreadable_rows, [runtime_file], exe, pid=777)
    assert mapping_permissions.cached_non_executable(777, unreadable_stamp, unreadable_path) is False


def test_verify_bound_service_reuses_asset_cache_when_bound_files_omitted(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    runtime_file = tmp_path / "server.py"
    runtime_file.write_text("print('ok')\n", encoding="utf-8")
    model_file = tmp_path / "model.gguf"
    model_file.write_bytes(b"gguf-bytes")

    binding_doc = {
        "schema_version": 3,
        "definition": "local-engine-binding.v1",
        "installation_sha256": "a" * 64,
        "service": {"pid": 1234, "origin": "http://127.0.0.1:48857"},
    }
    raw_binding = json_bytes(binding_doc)
    binding_path = tmp_path / "binding.json"
    binding_path.write_bytes(raw_binding)
    binding_sha = hashlib.sha256(raw_binding).hexdigest()

    config = {
        "endpoint": {
            "binding_path": str(binding_path),
            "binding_sha256": binding_sha,
        },
        "engine": {
            "family": "polystrata",
            "installation_path": str(tmp_path / "installation.json"),
            "installation_sha256": "a" * 64,
        },
        "model": {
            "local_path": str(model_file),
            "kind": "gguf",
            "assets_path": str(tmp_path / "assets.json"),
            "assets_sha256": "b" * 64,
            "content_sha256": "c" * 64,
            "template_path": str(tmp_path / "template.jinja"),
            "template_sha256": "d" * 64,
        },
    }

    monkeypatch.setattr(service, "_ASSET_CACHE", {})
    monkeypatch.setattr(service, "_require_family_platform", lambda _f: None)
    monkeypatch.setattr(service, "validate_binding", lambda doc, _cfg: doc)
    monkeypatch.setattr(service, "_bind", lambda _cfg, _rt, _rf: binding_doc)

    assets_calls = 0

    def fake_assets(_cfg) -> tuple[dict, list[FileIdentity], list[FileIdentity]]:
        nonlocal assets_calls
        assets_calls += 1
        return {"installation": {"family": "polystrata"}}, [hash_file(runtime_file)], [
            hash_file(model_file)
        ]

    monkeypatch.setattr(service, "assets", fake_assets)

    assert service.verify_bound_service(config) == binding_doc["service"]
    assert assets_calls == 1

    # Subsequent adapter._bound() calls (bound_files=None) reuse _ASSET_CACHE.
    assert service.verify_bound_service(config) == binding_doc["service"]
    assert service.verify_bound_service(config) == binding_doc["service"]
    assert assets_calls == 1

    # Mutating any tracked file invalidates stamp cache and triggers full re-inspection.
    runtime_file.write_text("print('mutated')\n", encoding="utf-8")
    assert service.verify_bound_service(config) == binding_doc["service"]
    assert assets_calls == 2
