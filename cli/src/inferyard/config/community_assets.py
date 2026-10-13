"""Read local model/engine assets without executing the engine or loading a model."""

import errno
import stat
from pathlib import Path

from inferyard.config.preparation_io import (
    NewDirectory,
    PreparationError,
    base_details,
    file_record,
    finish,
    new_path,
    require_platform,
    sha256,
)
from inferyard.platforms.gguf_metadata import read_metadata


def model_template(model):
    try:
        metadata = read_metadata(model)
    except ValueError, UnicodeError:
        raise PreparationError("invalid_input") from None
    template = metadata.get("tokenizer.chat_template") or metadata.get(
        "tokenizer.chat_template.default"
    )
    if not isinstance(template, str) or not template.strip():
        raise PreparationError("model_chat_template_required")
    try:
        return metadata, template.encode("utf-8")
    except UnicodeError:
        raise PreparationError("model_chat_template_required") from None


def libraries(engine, patterns):
    engine = Path(engine).resolve(strict=True)
    result = {engine.name: file_record(engine)["sha256"]}
    names = {engine.name.casefold()}
    for path in sorted({p for pattern in patterns for p in engine.parent.glob(pattern)}):
        try:
            resolved = path.resolve(strict=True)
            regular = stat.S_ISREG(resolved.stat().st_mode)
        except OSError as exc:
            if isinstance(exc, FileNotFoundError) or exc.errno == errno.ELOOP:
                raise PreparationError("invalid_library_manifest") from None
            raise
        if not resolved.is_relative_to(engine.parent) or not regular:
            raise PreparationError("invalid_library_manifest")
        if path.name.casefold() in names:
            raise PreparationError("invalid_library_manifest")
        names.add(path.name.casefold())
        result[path.name] = file_record(path)["sha256"]
    return result


def prepare(request):
    require_platform(("Linux", "x64"))
    new_path(request.out, (request.model_path, request.engine_path))
    model, engine = file_record(request.model_path), file_record(request.engine_path)
    _, template = model_template(model["path"])
    manifest = libraries(engine["path"], ("*.so", "*.so.*"))
    output = NewDirectory(request.out, (request.model_path, request.engine_path))
    path = output.write("chat-template.jinja", template)
    template_record = file_record(path)
    if template_record["sha256"] != sha256(template):
        raise PreparationError("serialization_mismatch", 4)
    output.json("engine-sha256.json", manifest)
    details = {
        **base_details(output.path),
        "model": model,
        "engine": engine,
        "template": template_record,
        "runtime_library_manifest": str(output.path / "engine-sha256.json"),
        "libraries": manifest,
        "assets": str(output.path / "assets.json"),
    }
    return finish(output, "assets.json", "config_assets.v1", details)
