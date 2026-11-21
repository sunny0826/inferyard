"""Multi-shard GGUF and directory-model (MLX/safetensors) acquisition helpers."""

from __future__ import annotations

from datetime import UTC, datetime
import os
from pathlib import Path, PurePosixPath
from urllib.parse import quote, urlencode

import httpx

from inferyard.contracts.json_digest import digest as canonical_digest
from inferyard.contracts.validation import strict_json_loads
from inferyard.errors import ContractError
from inferyard.platforms.model_assets import is_asset
from inferyard.platforms.output_tree import anchored
from inferyard.platforms.platform_io import sync_directory
from inferyard.provisioning.acquire_errors import (
    classify_http_status,
    classify_httpx_error,
    classify_os_error,
    failure_details,
)
from inferyard.provisioning.http_client import open_http_client, origin_of, validate_proxy_origin
from inferyard.provisioning.model_source import (
    DEFINITION_V3,
    PARSER,
    ModelDirectorySource,
    ModelSource,
    build_file_download_url,
    parse_directory_source,
    parse_gguf_shard_filename,
)
from inferyard.provisioning.preparation_io import PreparationError, new_path
from inferyard.provisioning.transfer_progress import (
    ProgressEmitter,
    ResumeDirectory,
    download_resumable_part,
    open_output_directory,
    parse_content_length,
)

INCOMPLETE = "model_acquire_incomplete"


def _now_utc() -> str:
    return datetime.now(UTC).isoformat(timespec="seconds").replace("+00:00", "Z")


def _safe_relpath(raw_path: str, prefix: str = "") -> str | None:
    if not isinstance(raw_path, str) or not raw_path or "\x00" in raw_path or "\\" in raw_path:
        return None
    if raw_path.startswith("/") or raw_path.endswith("/"):
        return None
    parts = raw_path.split("/")
    if any(part in ("", ".", "..") or ":" in part or part.startswith(".") for part in parts):
        return None
    if prefix:
        prefix_parts = prefix.split("/")
        if len(parts) <= len(prefix_parts) or parts[: len(prefix_parts)] != prefix_parts:
            return None
        parts = parts[len(prefix_parts) :]
    if any(part.startswith(".") for part in parts):
        return None
    return "/".join(parts)


def _part_name_for_relpath(rel_path: str, index: int, used: set[str]) -> str:
    candidate = f"{rel_path}.part"
    if "/" not in rel_path and candidate not in used:
        used.add(candidate)
        return candidate
    safe_base = Path(rel_path).name
    part = f"part_{index:04d}_{safe_base}.part"
    used.add(part)
    return part


def _probe_optional_size(client: httpx.Client, url: str, headers: dict[str, str]) -> int | None:
    try:
        resp = client.head(url, headers=headers)
        if resp.status_code in (200, 206):
            return parse_content_length(resp.headers)
    except httpx.HTTPError:
        pass
    return None


def _ensure_parent_dirs(output, rel_dir: PurePosixPath) -> None:
    if rel_dir == PurePosixPath("."):
        return
    if isinstance(output, ResumeDirectory):
        with output.check_context() if hasattr(output, "check_context") else anchored(
            output.path, output.identity
        ) as (tree, handle):
            for part in rel_dir.parts:
                handle = tree.descend(handle, part, create=True)
        return
    output.directory(Path(*rel_dir.parts))


def _download_staged_files(
    client: httpx.Client,
    output,
    items: list[tuple[str, str, str, int | None]],
    token: str | None,
    *,
    emitter: ProgressEmitter,
    resume: bool,
) -> tuple[list[dict], int]:
    headers = {"Accept-Encoding": "identity"}
    if token is not None:
        headers["Authorization"] = f"Bearer {token}"
    known_sizes = [item[3] for item in items]
    combined_total = sum(known_sizes) if known_sizes and all(isinstance(s, int) and s > 0 for s in known_sizes) else None
    offset = 0
    downloaded: list[tuple[str, str, int, str]] = []
    for rel_path, url, part_name, _expected_size in items:
        size, digest = download_resumable_part(
            client,
            url,
            output,
            part_name,
            incomplete_reason=INCOMPLETE,
            headers=headers,
            resume=resume,
            emitter=emitter,
            base_offset=offset,
            combined_total=combined_total,
        )
        offset += size
        downloaded.append((rel_path, part_name, size, digest))

    emitter.update("preparing", offset, combined_total or offset)
    entries: list[dict] = []
    for rel_path, part_name, size, digest in downloaded:
        rel_posix = PurePosixPath(rel_path)
        _ensure_parent_dirs(output, rel_posix.parent)
        os.rename(output.path / part_name, output.path.joinpath(*rel_posix.parts))
        entries.append({"path": rel_path, "sha256": digest, "size": size})
    sync_directory(output.path)
    entries.sort(key=lambda item: item["path"])
    return entries, offset


def acquire_sharded_gguf(
    source: ModelSource,
    out,
    token: str | None,
    *,
    timeout_factory,
    timeout_seconds,
    client=None,
    progress_stream=False,
    resume=False,
    proxy_origin=None,
) -> dict:
    """Download all 1..N shards of a multi-shard GGUF model set atomically."""
    filename = source.path.rsplit("/", 1)[-1]
    parsed_shard = parse_gguf_shard_filename(filename)
    if parsed_shard is None:
        raise PreparationError("invalid_model_source")
    prefix, _idx, total = parsed_shard
    remote_dir = source.path.rsplit("/", 1)[0] if "/" in source.path else ""
    shard_names = [f"{prefix}-{i:05d}-of-{total:05d}.gguf" for i in range(1, total + 1)]
    used_parts: set[str] = set()
    allowed_parts = {"source.json.part", "model-assets.json.part"}
    final_names = {"source.json", "model-assets.json", *shard_names}
    items_specs: list[tuple[str, str, str]] = []
    for idx, shard_name in enumerate(shard_names):
        part_name = _part_name_for_relpath(shard_name, idx, used_parts)
        allowed_parts.add(part_name)
        remote_path = f"{remote_dir}/{shard_name}" if remote_dir else shard_name
        url = build_file_download_url(source.platform, source.repository, source.revision, remote_path)
        items_specs.append((shard_name, url, part_name))

    validate_proxy_origin(proxy_origin)
    out = Path(out).absolute() if resume else new_path(out)
    emitter = ProgressEmitter(enabled=progress_stream)

    def _run(active: httpx.Client) -> dict:
        output = open_output_directory(
            out,
            resume=resume,
            allowed_parts=allowed_parts,
            final_names=final_names,
        )
        head_headers = {"Accept-Encoding": "identity"}
        if token is not None:
            head_headers["Authorization"] = f"Bearer {token}"
        items = [
            (rel_path, url, part_name, _probe_optional_size(active, url, head_headers))
            for rel_path, url, part_name in items_specs
        ]
        try:
            entries, total_bytes = _download_staged_files(
                active, output, items, token, emitter=emitter, resume=resume
            )
            manifest_sha = canonical_digest(entries)
            manifest = {
                "definition": "model-assets.v2",
                "path": str(output.path),
                "sha256": manifest_sha,
                "files": entries,
            }
            output.json("model-assets.json.part", manifest)
            os.rename(output.path / "model-assets.json.part", output.path / "model-assets.json")
            first_remote = f"{remote_dir}/{shard_names[0]}" if remote_dir else shard_names[0]
            record = {
                "definition": DEFINITION_V3,
                "kind": "gguf_sharded",
                "parser": PARSER,
                "platform": source.platform,
                "repository": source.repository,
                "revision": source.revision,
                "path": first_remote,
                "shards": entries,
                "bytes": total_bytes,
                "sha256": manifest_sha,
                "retrieved_at": _now_utc(),
            }
            output.json("source.json.part", record)
            os.rename(output.path / "source.json.part", output.path / "source.json")
            sync_directory(output.path)
        except BaseException:
            cleanup = (
                {"source.json.part", "model-assets.json.part"} if resume else allowed_parts
            )
            for part in cleanup:
                try:
                    (output.path / part).unlink()
                except OSError:
                    pass
            raise
        return {
            "out": str(output.path),
            "model": str(output.path / shard_names[0]),
            "kind": "gguf",
            "shards": entries,
            "source_record": str(output.path / "source.json"),
            "manifest": str(output.path / "model-assets.json"),
            "bytes": total_bytes,
            "sha256": manifest_sha,
            "ready_to_run": False,
            "model_requests_sent": 0,
        }

    try:
        if client is not None:
            return _run(client)
        auth_origin = origin_of(f"https://{source.host}") if token is not None else None
        with open_http_client(
            timeout_seconds,
            proxy_origin=proxy_origin,
            auth_origin=auth_origin,
            timeout_factory=timeout_factory,
        ) as owned:
            return _run(owned)
    except PreparationError:
        raise
    except httpx.HTTPError as exc:
        raise PreparationError(
            INCOMPLETE, details=failure_details("download_body", classify_httpx_error(exc))
        ) from None
    except OSError as exc:
        raise PreparationError(
            "io_error", 4, details=failure_details("disk_write", classify_os_error(exc))
        ) from None


def _list_directory_tree(
    client: httpx.Client,
    dir_source: ModelDirectorySource,
    token: str | None,
) -> list[tuple[str, str, int | None]]:
    headers = {"Accept": "application/json"}
    if token is not None:
        headers["Authorization"] = f"Bearer {token}"
    if dir_source.platform == "huggingface":
        suffix = f"/{quote(dir_source.subpath, safe='/')}" if dir_source.subpath else ""
        url = (
            f"https://huggingface.co/api/models/{dir_source.repository}"
            f"/tree/{dir_source.revision}{suffix}?recursive=true"
        )
    else:
        query = urlencode({"Revision": dir_source.revision, "Recursive": "True"})
        url = f"https://modelscope.cn/api/v1/models/{dir_source.repository}/repo/files?{query}"
    resp = client.get(url, headers=headers)
    if resp.status_code != 200:
        raise PreparationError(
            INCOMPLETE, details=failure_details("listing", classify_http_status(resp.status_code))
        )
    try:
        payload = strict_json_loads(resp.text)
    except ContractError, ValueError:
        raise PreparationError(
            INCOMPLETE, details=failure_details("listing", "upstream_http_error")
        ) from None

    raw_items: list[dict]
    if dir_source.platform == "huggingface":
        if not isinstance(payload, list):
            raise PreparationError(
                INCOMPLETE, details=failure_details("listing", "upstream_http_error")
            )
        raw_items = [item for item in payload if isinstance(item, dict) and item.get("type") == "file"]
    else:
        if not isinstance(payload, dict) or payload.get("Success") is False:
            raise PreparationError(
                INCOMPLETE, details=failure_details("listing", "upstream_http_error")
            )
        data = payload.get("Data") if isinstance(payload.get("Data"), dict) else payload
        files = data.get("Files")
        if not isinstance(files, list):
            raise PreparationError(
                INCOMPLETE, details=failure_details("listing", "upstream_http_error")
            )
        raw_items = [
            item
            for item in files
            if isinstance(item, dict) and item.get("Type") in (None, "blob", "file")
        ]

    selected: list[tuple[str, str, int | None]] = []
    seen_rel: set[str] = set()
    for item in raw_items:
        repo_path = item.get("path") if dir_source.platform == "huggingface" else item.get("Path")
        if not isinstance(repo_path, str):
            continue
        if "\\" in repo_path or repo_path.startswith("/") or ".." in repo_path.split("/"):
            raise PreparationError(
                INCOMPLETE, details=failure_details("listing", "invalid_directory_entry")
            )
        rel_path = _safe_relpath(repo_path, dir_source.subpath)
        if rel_path is None or rel_path in seen_rel:
            continue
        if not is_asset(Path(rel_path)):
            continue
        seen_rel.add(rel_path)
        raw_size = item.get("size") if dir_source.platform == "huggingface" else item.get("Size")
        if raw_size is None and isinstance(item.get("lfs"), dict):
            raw_size = item["lfs"].get("size")
        size = int(raw_size) if type(raw_size) is int and raw_size > 0 else None
        dl_url = build_file_download_url(
            dir_source.platform, dir_source.repository, dir_source.revision, repo_path
        )
        selected.append((rel_path, dl_url, size))

    rel_names = {rel.lower() for rel, _, _ in selected}
    has_config = "config.json" in rel_names
    has_tokenizer = any(
        n in ("tokenizer.json", "tokenizer.model", "tokenizer_config.json")
        or n.endswith("/tokenizer.json")
        for n in rel_names
    )
    has_weights = any(
        n.endswith(".safetensors") or (Path(n).name.startswith("pytorch_model") and n.endswith(".bin"))
        for n in rel_names
    )
    if not (has_config and has_tokenizer and has_weights):
        raise PreparationError(
            INCOMPLETE, details=failure_details("listing", "incomplete_model_directory")
        )
    selected.sort(key=lambda item: item[0])
    return selected


def acquire_directory_model(
    url: str,
    out,
    token: str | None,
    *,
    timeout_factory,
    timeout_seconds,
    client=None,
    progress_stream=False,
    resume=False,
    proxy_origin=None,
) -> dict:
    """Download a pinned HF/ModelScope directory model (MLX/safetensors) atomically."""
    dir_source = parse_directory_source(url, allow_file_parent=True)
    validate_proxy_origin(proxy_origin)
    out = Path(out).absolute() if resume else new_path(out)
    emitter = ProgressEmitter(enabled=progress_stream)

    def _run(active: httpx.Client) -> dict:
        tree_items = _list_directory_tree(active, dir_source, token)
        used_parts: set[str] = set()
        allowed_parts = {"source.json.part", "model-assets.json.part"}
        final_names = {"source.json", "model-assets.json"}
        staged_items: list[tuple[str, str, str, int | None]] = []
        for idx, (rel_path, dl_url, size) in enumerate(tree_items):
            part_name = _part_name_for_relpath(rel_path, idx, used_parts)
            allowed_parts.add(part_name)
            final_names.add(rel_path.split("/", 1)[0])
            staged_items.append((rel_path, dl_url, part_name, size))
        output = open_output_directory(
            out,
            resume=resume,
            allowed_parts=allowed_parts,
            final_names=final_names,
        )
        try:
            entries, total_bytes = _download_staged_files(
                active, output, staged_items, token, emitter=emitter, resume=resume
            )
            manifest_sha = canonical_digest(entries)
            manifest = {
                "definition": "model-assets.v2",
                "path": str(output.path),
                "sha256": manifest_sha,
                "files": entries,
            }
            output.json("model-assets.json.part", manifest)
            os.rename(output.path / "model-assets.json.part", output.path / "model-assets.json")
            record = {
                "definition": DEFINITION_V3,
                "kind": "directory",
                "parser": PARSER,
                "platform": dir_source.platform,
                "repository": dir_source.repository,
                "revision": dir_source.revision,
                "path": dir_source.subpath,
                "bytes": total_bytes,
                "sha256": manifest_sha,
                "retrieved_at": _now_utc(),
            }
            output.json("source.json.part", record)
            os.rename(output.path / "source.json.part", output.path / "source.json")
            sync_directory(output.path)
        except BaseException:
            cleanup = (
                {"source.json.part", "model-assets.json.part"} if resume else allowed_parts
            )
            for part in cleanup:
                try:
                    (output.path / part).unlink()
                except OSError:
                    pass
            raise
        return {
            "out": str(output.path),
            "model": str(output.path),
            "kind": "directory",
            "files": len(entries),
            "source_record": str(output.path / "source.json"),
            "manifest": str(output.path / "model-assets.json"),
            "bytes": total_bytes,
            "sha256": manifest_sha,
            "ready_to_run": False,
            "model_requests_sent": 0,
        }

    try:
        if client is not None:
            return _run(client)
        auth_origin = origin_of(f"https://{dir_source.host}") if token is not None else None
        with open_http_client(
            timeout_seconds,
            proxy_origin=proxy_origin,
            auth_origin=auth_origin,
            timeout_factory=timeout_factory,
        ) as owned:
            return _run(owned)
    except PreparationError:
        raise
    except httpx.HTTPError as exc:
        raise PreparationError(
            INCOMPLETE, details=failure_details("download_body", classify_httpx_error(exc))
        ) from None
    except OSError as exc:
        raise PreparationError(
            "io_error", 4, details=failure_details("disk_write", classify_os_error(exc))
        ) from None
