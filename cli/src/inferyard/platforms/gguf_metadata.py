"""Read bounded GGUF metadata without loading model tensors or executing templates."""

import os
import struct
from pathlib import Path

FORMATS = {
    0: "B",
    1: "b",
    2: "H",
    3: "h",
    4: "I",
    5: "i",
    6: "f",
    7: "?",
    10: "Q",
    11: "q",
    12: "d",
}
LIMIT = 64 * 1024**2


def read_metadata(path, *, extra_keys=()):
    path = Path(path)
    with path.open("rb") as stream:
        size = os.fstat(stream.fileno()).st_size

        def read(count):
            if count < 0 or stream.tell() + count > min(size, LIMIT):
                raise ValueError("gguf_metadata_bounds")
            value = stream.read(count)
            if len(value) != count:
                raise ValueError("truncated_gguf_metadata")
            return value

        def scalar(fmt):
            return struct.unpack("<" + fmt, read(struct.calcsize(fmt)))[0]

        def string(keep):
            count = scalar("Q")
            if count > 16 * 1024**2:
                raise ValueError("gguf_string_too_large")
            if keep:
                return read(count).decode("utf-8")
            if stream.tell() + count > min(size, LIMIT):
                raise ValueError("gguf_metadata_bounds")
            stream.seek(count, 1)
            return None

        def value(kind, keep):
            if kind in FORMATS:
                result = scalar(FORMATS[kind])
                return result if keep else None
            if kind == 8:
                return string(keep)
            if kind == 9:
                item_kind, count = scalar("I"), scalar("Q")
                if count > 10_000_000 or item_kind == 9:
                    raise ValueError("unsupported_gguf_array")
                if item_kind in FORMATS:
                    skip = count * struct.calcsize(FORMATS[item_kind])
                    if stream.tell() + skip > min(size, LIMIT):
                        raise ValueError("gguf_metadata_bounds")
                    stream.seek(skip, 1)
                elif item_kind == 8:
                    for _ in range(count):
                        string(False)
                else:
                    raise ValueError("unsupported_gguf_value_type")
                return None
            raise ValueError("unsupported_gguf_value_type")

        if read(4) != b"GGUF" or scalar("I") not in (2, 3):
            raise ValueError("unsupported_gguf_header")
        tensors, count = scalar("Q"), scalar("Q")
        if not tensors or count > 100_000:
            raise ValueError("invalid_gguf_header")
        metadata = {}
        keys = set()
        for _ in range(count):
            key, kind = string(True), scalar("I")
            if key in keys:
                raise ValueError("duplicate_gguf_metadata_key")
            keys.add(key)
            if key in extra_keys and kind == 9:
                raise ValueError("unsupported_gguf_metadata_array")
            keep = key in (
                "general.name",
                "general.architecture",
                "general.file_type",
                "tokenizer.chat_template",
                "tokenizer.chat_template.default",
                *extra_keys,
            ) or key.endswith((".context_length", ".block_count"))
            result = value(kind, keep)
            if keep:
                metadata[key] = result
        return metadata
