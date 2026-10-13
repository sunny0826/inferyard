"""Variants of the operator-reported NInfer stream shape; synthetic values."""

import json
from pathlib import Path


def usage_stream(mode="object", *, lab=False):
    text = (Path(__file__).parent / "fixtures/native/ninfer-usage-null.sse").read_text()
    frames = []
    for frame in text.strip().split("\n\n"):
        payload = frame.removeprefix("data: ")
        if payload != "[DONE]":
            value = json.loads(payload)
            if mode == "null":
                value["usage"] = None
            elif mode == "absent":
                value.pop("usage", None)
            elif mode == "invalid":
                value["usage"] = "not-an-object"
            if lab:
                value.update(lab_request_id="2" * 32, lab_server_instance_id="1" * 32)
            payload = json.dumps(value, ensure_ascii=False)
        frames.append(("data: " + payload + "\n\n").encode())
    return frames
