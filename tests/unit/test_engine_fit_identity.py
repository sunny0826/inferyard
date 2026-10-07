import hashlib
import os
from pathlib import Path

import pytest

from inferyard.evidence.storage import json_bytes
from inferyard.platforms import engine_fit as fit
from inferyard.platforms.identity import PreflightError


def test_manifest_follows_hf_file_links_and_hashes_sorted_relative_files(tmp_path):
    model = tmp_path / "snapshot"
    model.mkdir()
    blob = tmp_path / "blob"
    blob.write_bytes(b"model weights")
    (model / "weights.safetensors").symlink_to(blob)
    (model / "config.json").write_bytes(b"{}")
    manifest = fit.model_manifest(model)
    assert manifest["path"] == str(model.resolve())
    assert [row["path"] for row in manifest["files"]] == ["config.json", "weights.safetensors"]
    assert manifest["sha256"] == hashlib.sha256(json_bytes(manifest["files"])).hexdigest()
    assert manifest["files"][1]["sha256"] == hashlib.sha256(blob.read_bytes()).hexdigest()


@pytest.mark.parametrize("kind", ["directory_link", "fifo", "broken_link", "empty", "file"])
def test_manifest_rejects_unsafe_or_empty_model_directories(tmp_path, kind):
    model = tmp_path / "snapshot"
    model.mkdir()
    if kind == "directory_link":
        (model / "nested").symlink_to(tmp_path, target_is_directory=True)
    elif kind == "fifo":
        os.mkfifo(model / "weights")
    elif kind == "broken_link":
        (model / "missing").symlink_to(tmp_path / "does-not-exist")
    elif kind == "file":
        model = model / "weights"
        model.write_bytes(b"file")
    with pytest.raises(PreflightError):
        fit.model_manifest(model)


def test_manifest_rejects_root_directory_link(tmp_path):
    model = tmp_path / "snapshot"
    model.mkdir()
    (model / "weights").write_bytes(b"model")
    link = tmp_path / "link"
    link.symlink_to(model, target_is_directory=True)
    with pytest.raises(PreflightError, match="directory_symlink"):
        fit.model_manifest(link)


@pytest.mark.parametrize("change", ["content", "new_file", "retarget_link"])
def test_manifest_detects_mutation_during_hash(tmp_path, monkeypatch, change):
    model = tmp_path / "snapshot"
    model.mkdir()
    blob = tmp_path / "blob"
    blob.write_bytes(b"weights")
    (model / "weights").symlink_to(blob)
    original = fit.hashlib.file_digest

    def mutate(stream, name):
        result = original(stream, name)
        if change == "content":
            blob.write_bytes(b"changed")
        elif change == "new_file":
            (model / "new").write_bytes(b"new")
        else:
            other = tmp_path / "other"
            other.write_bytes(b"weights")
            (model / "weights").unlink()
            (model / "weights").symlink_to(other)
        return result

    monkeypatch.setattr(fit.hashlib, "file_digest", mutate)
    with pytest.raises(PreflightError, match="changed"):
        fit.model_manifest(model)


def test_host_hash_uses_machine_identity_and_hardware_without_disclosing_id(monkeypatch):
    from inferyard.platforms import device_host

    monkeypatch.setattr(fit.platform, "system", lambda: "Linux")
    machine_id = "11" * 16
    monkeypatch.setattr(fit, "_machine_identity", lambda system: (machine_id, "machine-id"))
    hardware = {"cpu_model": "test CPU", "logical_cpus": 8, "memory_total_bytes": 2**30}
    monkeypatch.setattr(device_host, "snapshot", lambda system: hardware)
    first = fit.host_identity()
    assert machine_id not in repr(first)
    monkeypatch.setattr(fit.platform, "node", lambda: "same-hostname-is-not-identity")
    assert fit.host_identity() == first
    machine_id = "22" * 16
    assert fit.host_identity()["sha256"] != first["sha256"]
    machine_id = "11" * 16
    hardware["memory_total_bytes"] *= 2
    assert fit.host_identity()["sha256"] != first["sha256"]


def test_linux_machine_identity_requires_nonzero_machine_id(monkeypatch):
    monkeypatch.setattr(Path, "read_text", lambda self: "0" * 32)
    with pytest.raises(PreflightError, match="host_identity_unavailable"):
        fit._machine_identity("Linux")
    monkeypatch.setattr(Path, "read_text", lambda self: "a" * 32)
    assert fit._machine_identity("Linux") == ("a" * 32, "/etc/machine-id")


def _process(proc, pid, *, parent=0, ticks=67890, rss=2, children=()):
    directory = proc / str(pid)
    directory.mkdir(exist_ok=True)
    fields = ["S"] + ["0"] * 23
    for index, value in ((1, parent), (11, 50), (12, 25), (19, ticks), (21, rss)):
        fields[index] = str(value)
    (directory / "stat").write_text(f"{pid} (server with ) spaces) " + " ".join(fields))
    task = directory / "task" / str(pid)
    task.mkdir(parents=True, exist_ok=True)
    (task / "children").write_text(" ".join(map(str, children)))
    return directory


def _cmdline(directory, *args):
    (directory / "cmdline").write_bytes(b"\0".join(os.fsencode(arg) for arg in (*args, "")))


@pytest.fixture
def service(tmp_path):
    if os.name == "nt":
        pytest.skip("Linux /proc fixtures require POSIX symlinks and uid")
    proc = tmp_path / "proc"
    proc.mkdir()
    (proc / "meminfo").write_text("MemAvailable: 2048 kB\n")
    directory = _process(proc, 123)
    (directory / "fd").mkdir()
    (directory / "net").mkdir()
    model = tmp_path / "model"
    model.mkdir()
    (model / "weights").write_bytes(b"model")
    binary = tmp_path / "python3"
    binary.write_bytes(b"python executable fixture")
    (directory / "exe").symlink_to(binary)
    (directory / "cwd").symlink_to(tmp_path)
    _cmdline(directory, "python3", "-m", "vllm.entrypoints.openai.api_server", "--model", model)
    (directory / "fd/5").symlink_to("socket:[456]")
    (directory / "net/tcp").write_text(
        "sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n"
        "0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 456\n"
    )
    return proc, directory, model


@pytest.mark.parametrize(
    "engine,args",
    [
        ("vllm", ["vllm", "serve", "model"]),
        ("vllm", ["python3", "/bin/vllm", "serve", "model"]),
        ("vllm", ["python3.14", "-u", "-m", "vllm.entrypoints.openai.api_server", "--model=model"]),
        ("sglang", ["python", "-m", "sglang.launch_server", "--model-path", "model"]),
    ],
)
def test_binding_supported_entrypoints_and_relative_model_directory(service, engine, args):
    proc, directory, model = service
    _cmdline(directory, *args, "--api-key", "fixture-secret")
    binding = fit.bind_service(engine, model, 123, "http://127.0.0.1:8080", proc_root=proc)
    assert binding["model_binding"]["path"] == str(model.resolve())
    assert binding["model_binding"]["engine"] == engine
    assert binding["start_ticks"] == 67890
    assert binding["listener_inode"] == "456"
    assert "fixture-secret" not in repr(binding)
    assert "startup_args" not in binding
    fit.check_service(binding, proc_root=proc)


@pytest.mark.parametrize(
    "args",
    [
        ["python3", "other.py", "vllm.entrypoints.openai.api_server", "--model", "model"],
        [
            "python3",
            "-m",
            "other.module",
            "--label",
            "vllm.entrypoints.openai.api_server",
            "--model",
            "model",
        ],
        ["python3", "-c", "print('vllm')", "vllm", "serve", "model"],
        ["vllm", "serve", "model", "--model", "other"],
        [
            "python3",
            "-m",
            "vllm.entrypoints.openai.api_server",
            "--model",
            "model",
            "--model=model",
        ],
    ],
)
def test_arbitrary_module_strings_and_ambiguous_model_arguments_cannot_bind(service, args):
    proc, directory, model = service
    _cmdline(directory, *args)
    with pytest.raises(PreflightError):
        fit.bind_service("vllm", model, 123, "http://127.0.0.1:8080", proc_root=proc)


@pytest.mark.parametrize("change", ["pid", "argv", "binary", "listener", "model_inode", "cwd"])
def test_check_service_detects_identity_changes(service, change):
    proc, directory, model = service
    _cmdline(directory, "vllm", "serve", "model")
    binding = fit.bind_service("vllm", model, 123, "http://127.0.0.1:8080", proc_root=proc)
    if change == "pid":
        _process(proc, 123, ticks=67891)
    elif change == "argv":
        _cmdline(directory, "vllm", "serve", "model", "--dtype", "float32")
    elif change == "binary":
        (directory / "exe").write_bytes(b"changed interpreter")
    elif change == "listener":
        (directory / "fd/5").unlink()
    elif change == "model_inode":
        model.rename(model.with_name("old-model"))
        model.mkdir()
    else:
        (directory / "cwd").unlink()
        (directory / "cwd").symlink_to(model)
    with pytest.raises(PreflightError):
        fit.check_service(binding, proc_root=proc)


def test_remote_endpoint_and_unsupported_windows_engine_binding_rejected(service, monkeypatch):
    proc, _, model = service
    with pytest.raises(PreflightError):
        fit.bind_service("vllm", model, 123, "http://192.0.2.1:8080", proc_root=proc)
    monkeypatch.setattr(fit.platform, "system", lambda: "Windows")
    with pytest.raises(PreflightError, match="windows_engine_unavailable"):
        fit.bind_service("vllm", model, 123, "http://127.0.0.1:8080")


def test_listener_reuse_by_another_process_is_ambiguous(service):
    proc, directory, model = service
    table = directory / "net/tcp"
    table.write_text(
        table.read_text()
        + "1: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 999\n"
    )
    with pytest.raises(PreflightError, match="listener_ambiguous"):
        fit.bind_service("vllm", model, 123, "http://127.0.0.1:8080", proc_root=proc)


def test_executable_content_not_basename_is_bound(service):
    proc, directory, model = service
    binary = model.parent / "unrelated-program"
    binary.write_bytes(b"unrelated executable")
    (directory / "exe").unlink()
    (directory / "exe").symlink_to(binary)
    binding = fit.bind_service("vllm", model, 123, "http://127.0.0.1:8080", proc_root=proc)
    assert binding["executable_sha256"] == hashlib.sha256(binary.read_bytes()).hexdigest()
    binary.write_bytes(b"changed executable")
    with pytest.raises(PreflightError, match="identity_changed"):
        fit.check_service(binding, proc_root=proc)


def test_resource_snapshot_includes_children_from_all_threads(service, monkeypatch):
    proc, _, model = service
    binding = fit.bind_service("vllm", model, 123, "http://127.0.0.1:8080", proc_root=proc)
    thread = proc / "123/task/124"
    thread.mkdir()
    (thread / "children").write_text("456")
    _process(proc, 456, parent=123, children=[789])
    _process(proc, 789, parent=456)
    monkeypatch.setattr(fit.os, "sysconf", lambda key: 100 if key == "SC_CLK_TCK" else 4096)
    snapshot = fit.resource_snapshot(binding, proc_root=proc)
    assert snapshot["memory_available_bytes"] == 2048 * 1024
    assert snapshot["process_count"] == 3
    assert snapshot["process_tree_rss_bytes"] == 6 * 4096
    assert snapshot["process_tree_cpu_seconds"] == 2.25
    assert snapshot["missing_reasons"] == {}
    assert "shared_pages" in snapshot["scope"]["rss"]


def test_resource_snapshot_detects_child_replacement_mid_sample(service, monkeypatch):
    proc, _, model = service
    binding = fit.bind_service("vllm", model, 123, "http://127.0.0.1:8080", proc_root=proc)
    _process(proc, 123, children=[456])
    _process(proc, 456, parent=123)
    original = fit._process_row
    calls = 0

    def replacing_child(pid, root):
        nonlocal calls
        calls += 1
        if calls == 3:
            _process(proc, 456, parent=123, ticks=1)
        return original(pid, root)

    monkeypatch.setattr(fit, "_process_row", replacing_child)
    snapshot = fit.resource_snapshot(binding, proc_root=proc)
    assert snapshot["process_tree_rss_bytes"] is None
    assert (
        snapshot["missing_reasons"]["process_tree_rss_bytes"] == "engine_fit_process_tree_changed"
    )


@pytest.mark.parametrize("change", ["root_reused", "child_missing", "child_reparented", "bad_rss"])
def test_unreadable_or_changed_process_tree_is_missing_not_partial(service, change):
    proc, _, model = service
    binding = fit.bind_service("vllm", model, 123, "http://127.0.0.1:8080", proc_root=proc)
    if change == "root_reused":
        _process(proc, 123, ticks=1)
    elif change == "bad_rss":
        _process(proc, 123, rss=-1)
    else:
        _process(proc, 123, children=[456])
        if change == "child_reparented":
            _process(proc, 456, parent=999)
    snapshot = fit.resource_snapshot(binding, proc_root=proc)
    for field in ("process_tree_rss_bytes", "process_tree_cpu_seconds", "process_count"):
        assert snapshot[field] is None
        assert snapshot["missing_reasons"][field]
    assert snapshot["memory_available_bytes"] == 2048 * 1024


def test_asset_v2_excludes_documentation_cache_and_location(tmp_path, monkeypatch):
    from inferyard.config.engine_fit_assets import model_asset_manifest
    from inferyard.runtime.engine_fit import _manifest

    root = tmp_path / "first"
    root.mkdir()
    for name in ("model.safetensors", "config.json", "tokenizer.json", "chat_template.jinja"):
        (root / name).write_bytes(b"asset")
    (root / "README.md").write_bytes(b"readme")
    cache = root / ".cache"
    cache.mkdir()
    (cache / "weights.safetensors").write_bytes(b"cache")
    reads = []
    original = fit._file_hash

    def counted(path):
        reads.append(path.name)
        return original(path)

    monkeypatch.setattr(fit, "_file_hash", counted)
    first = model_asset_manifest(root)
    assert len(reads) == 4
    assert set(reads) == {
        "model.safetensors",
        "config.json",
        "tokenizer.json",
        "chat_template.jinja",
    }
    (root / "README.md").write_bytes(b"changed")
    (cache / "more.bin").write_bytes(b"cache")
    assert model_asset_manifest(root) == first
    old = model_asset_manifest(root, definition=None)
    assert "definition" not in old
    assert len(old["files"]) == 7
    assert _manifest({"definition": "engine_fit_plan.v4", "model": old}) == old
    relocated = tmp_path / "second"
    root.rename(relocated)
    second = model_asset_manifest(relocated)
    assert second["sha256"] == first["sha256"]
    (relocated / "tokenizer.json").write_bytes(b"different")
    assert model_asset_manifest(relocated)["sha256"] != first["sha256"]


@pytest.mark.parametrize("definition", [None, "model-assets.v2"])
@pytest.mark.parametrize("change", ["content", "add", "delete"])
def test_added_token_vocabulary_is_bound_in_frozen_and_runtime_assets(tmp_path, definition, change):
    from inferyard.config.engine_fit_assets import model_asset_manifest
    from inferyard.runtime.engine_fit import _manifest

    (tmp_path / "model.safetensors").write_bytes(b"weights")
    (tmp_path / "tokenizer_config.json").write_text("{}")
    extra = tmp_path / "added_tokens.json"
    if change != "add":
        extra.write_text('{"alpha":100}')
    before = model_asset_manifest(tmp_path, definition=definition)
    plan = {"definition": "engine_fit_plan.v4", "model": before}
    assert _manifest(plan) == before
    if change == "delete":
        extra.unlink()
    else:
        extra.write_text('{"beta":100}')
    after = model_asset_manifest(tmp_path, definition=definition)
    assert before["sha256"] != after["sha256"]
    assert _manifest(plan) == after
    assert (extra.name in {row["path"] for row in after["files"]}) == (change != "delete")


def test_asset_v2_detects_selected_file_added_during_hash(tmp_path, monkeypatch):
    (tmp_path / "model.safetensors").write_bytes(b"weights")
    original = fit._file_hash

    def added(path):
        value = original(path)
        (tmp_path / "tokenizer.json").write_bytes(b"{}")
        return value

    monkeypatch.setattr(fit, "_file_hash", added)
    with pytest.raises(PreflightError, match="model_changed_during_hash"):
        fit.model_manifest(tmp_path, definition="model-assets.v2")


def test_asset_v2_hashes_every_index_referenced_weight(tmp_path):
    import json

    index = tmp_path / "model.safetensors.index.json"
    index.write_text(json.dumps({"weight_map": {"tensor": "part-00001.bin"}}))
    weight = tmp_path / "part-00001.bin"
    weight.write_bytes(b"first weights")
    first = fit.model_manifest(tmp_path, definition="model-assets.v2")
    assert [r["path"] for r in first["files"]] == [index.name, weight.name]
    weight.write_bytes(b"changed weights")
    assert fit.model_manifest(tmp_path, definition="model-assets.v2")["sha256"] != first["sha256"]


def test_asset_v2_rejects_missing_or_outside_index_references(tmp_path):
    import json

    index = tmp_path / "model.safetensors.index.json"
    for name in (
        "../outside.bin",
        "/absolute.bin",
        "C:\\outside.bin",
        "missing.bin",
        ".cache/weight.bin",
    ):
        index.write_text(json.dumps({"weight_map": {"tensor": name}}))
        with pytest.raises(PreflightError):
            fit.model_manifest(tmp_path, definition="model-assets.v2")
