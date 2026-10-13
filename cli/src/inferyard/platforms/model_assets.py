"""Versioned directory asset selection; locators are not content identities."""

import stat
from pathlib import PurePosixPath

from inferyard.platforms.identity import PreflightError

DEFINITION = "model-assets.v2"
IGNORED_DIRECTORIES = {".git", ".cache", "__pycache__", "cache", "logs"}


def is_asset(path):
    name = path.name.lower()
    return (
        path.suffix.lower() in {".safetensors", ".gguf", ".pt", ".pth", ".npz", ".py"}
        or name.endswith(".index.json")
        or name
        in {"config.json", "generation_config.json", "special_tokens_map.json", "added_tokens.json"}
        or name.startswith(("tokenizer", "vocab", "merges", "spiece", "chat_template"))
        or (name.endswith(".bin") and name.startswith(("model", "pytorch_model", "weights")))
        or path.suffix.lower() in {".jinja", ".jinja2", ".tiktoken"}
    )


def inventory(root, *, frozen_paths=None):
    from inferyard.platforms.engine_fit import _stamp

    result, pending = {}, [root]
    while pending:
        directory = pending.pop()
        if directory.is_symlink():
            raise PreflightError("engine_fit_directory_symlink")
        for entry in sorted(directory.iterdir()):
            if entry.name in IGNORED_DIRECTORIES:
                continue
            if entry.is_dir():
                if entry.is_symlink():
                    raise PreflightError("engine_fit_directory_symlink")
                pending.append(entry)
            elif is_asset(entry):
                link, target = entry.lstat(), entry.stat()
                if not stat.S_ISREG(target.st_mode):
                    raise PreflightError("engine_fit_not_regular_file")
                result[entry.relative_to(root).as_posix()] = (_stamp(link), _stamp(target))
    references = frozen_paths if frozen_paths is not None else _index_paths(root, result)
    for name in references:
        path = root / name
        link, target = path.lstat(), path.stat()
        if not stat.S_ISREG(target.st_mode):
            raise PreflightError("engine_fit_not_regular_file")
        result.setdefault(name, (_stamp(link), _stamp(target)))
    return result


def _index_paths(root, selected):
    from inferyard.contracts.validation import strict_json_loads

    references = set()
    for name in selected:
        if not name.lower().endswith(".index.json"):
            continue
        index = strict_json_loads((root / name).read_text(encoding="utf-8"))
        mapping = index.get("weight_map") if isinstance(index, dict) else None
        if not isinstance(mapping, dict) or not mapping:
            raise PreflightError("engine_fit_model_index_invalid")
        for value in mapping.values():
            if (
                not isinstance(value, str)
                or not value
                or PurePosixPath(value).is_absolute()
                or "\\" in value
                or ":" in value
                or any(p in ("", ".", "..") or p in IGNORED_DIRECTORIES for p in value.split("/"))
            ):
                raise PreflightError("engine_fit_model_index_invalid")
            # Index paths are relative to the index directory, not the command cwd.
            references.add((PurePosixPath(name).parent / value).as_posix())
    return references
