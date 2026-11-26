"""Frozen model asset manifests for diagnostic plans and single-file models."""

from pathlib import Path

from inferyard.contracts.json_digest import digest
from inferyard.errors import PreflightError, UnsupportedFormat


def runtime_manifest(plan):
    """Resolve the frozen asset algorithm, without importing the live runner."""
    from inferyard.platforms.engine_fit import model_manifest

    path = Path(plan["model"]["path"])
    if plan["definition"] != "engine_fit_plan.v1":
        return model_asset_manifest(path, definition=plan["model"].get("definition"))
    return model_manifest(path)


def _single_gguf_entry(target, *, expected_count=None, expected_index=None):
    from inferyard.platforms.engine_fit import file_stamp, stable_file_digest
    from inferyard.platforms.gguf_metadata import read_metadata

    resolved = target.resolve(strict=True)
    if not resolved.is_file() or resolved.suffix.lower() != ".gguf":
        raise PreflightError("engine_fit_single_gguf_required")
    before = file_stamp(resolved.stat())
    sha, size = stable_file_digest(resolved)
    metadata = read_metadata(resolved, extra_keys=("split.count", "split.no"))
    count, index = metadata.get("split.count"), metadata.get("split.no")
    if expected_count is not None:
        if (count is not None and (type(count) is not int or count != expected_count)) or (
            index is not None and (type(index) is not int or index != expected_index)
        ):
            raise PreflightError("engine_fit_split_gguf_unsupported")
    elif (count is not None and type(count) is not int) or (
        index is not None and type(index) is not int
    ):
        raise PreflightError("engine_fit_split_gguf_unsupported")
    if file_stamp(resolved.stat()) != before:
        raise PreflightError("engine_fit_model_changed_during_hash")
    entry_name = target.name if expected_count is not None else resolved.name
    return resolved, {"path": entry_name, "sha256": sha, "size": size}, count, index


def _multi_shard_gguf_entries(root, total):
    from inferyard.platforms.asset_scan import parse_gguf_shard_filename

    parsed = parse_gguf_shard_filename(root.name)
    if parsed is None or parsed[1] != 1 or parsed[2] != total:
        raise PreflightError("engine_fit_split_gguf_incomplete")
    prefix = parsed[0]
    entries = []
    for shard_no in range(1, total + 1):
        shard_path = root.parent / f"{prefix}-{shard_no:05d}-of-{total:05d}.gguf"
        if not shard_path.exists() and not shard_path.is_symlink():
            raise PreflightError("engine_fit_split_gguf_incomplete")
        _, entry, _, _ = _single_gguf_entry(
            shard_path, expected_count=total, expected_index=shard_no - 1
        )
        entries.append(entry)
    return entries


def model_asset_manifest(path, *, definition="model-assets.v2"):
    from inferyard.platforms.engine_fit import model_manifest

    path = Path(path)
    if path.is_dir():
        from inferyard.contracts.format_versions import require_version

        if definition is None:
            raise UnsupportedFormat("model-assets", "unversioned", ("model-assets.v2",))

        require_version(
            {"definition": definition}, "definition", ("model-assets.v2",), "model-assets"
        )
        return {"kind": "directory", **model_manifest(path, definition=definition)}
    try:
        root, entry, count, index = _single_gguf_entry(path)
        if index is not None and index != 0:
            raise PreflightError("engine_fit_split_gguf_unsupported")
        if count is not None and count != 1:
            if count < 1:
                raise PreflightError("engine_fit_split_gguf_unsupported")
            entries = _multi_shard_gguf_entries(root, count)
        else:
            entries = [entry]
        return {"kind": "gguf", "path": str(root), "sha256": digest(entries), "files": entries}
    except PreflightError:
        raise
    except (OSError, ValueError) as exc:
        raise PreflightError("engine_fit_model_unreadable") from exc
