import hashlib
import importlib.util
from pathlib import Path

import pytest

SPEC = importlib.util.spec_from_file_location(
    "verify_build_inputs", Path(__file__).parents[2] / "scripts/verify_build_inputs.py"
)
module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(module)


def fixture(tmp_path):
    (tmp_path / "config.json").write_bytes(b"{}")
    (tmp_path / "weights").write_bytes(b"abcd")
    return {
        "declared_total_bytes": 6,
        "files": [
            {
                "rfilename": "config.json",
                "size": 2,
                "blobId": hashlib.sha1(b"blob 2\0{}").hexdigest(),
            },
            {
                "rfilename": "weights",
                "size": 4,
                "lfs": {"size": 4, "sha256": hashlib.sha256(b"abcd").hexdigest()},
            },
        ],
    }


def test_both_publisher_hash_types(tmp_path):
    rows = module.verify_base_files(fixture(tmp_path), tmp_path)
    assert len(rows) == 2
    assert sum(row["bytes"] for row in rows) == 6


@pytest.mark.parametrize("change", ["partial", "missing", "corrupt", "symlink", "size"])
def test_invalid_input_rejected(tmp_path, change):
    plan = fixture(tmp_path)
    weight = tmp_path / "weights"
    if change == "partial":
        weight.rename(tmp_path / "weights.partial")
    elif change == "missing":
        weight.unlink()
    elif change == "corrupt":
        weight.write_bytes(b"abce")
    elif change == "symlink":
        weight.unlink()
        from tests.helpers import symlink_or_skip

        symlink_or_skip(weight, tmp_path / "config.json")
    else:
        weight.write_bytes(b"abc")
    with pytest.raises(ValueError):
        module.verify_base_files(plan, tmp_path)


def test_unexpected_python_input_rejected(tmp_path):
    plan = fixture(tmp_path)
    (tmp_path / "custom_model.py").write_text("raise RuntimeError()")
    with pytest.raises(ValueError, match="unexpected_files"):
        module.verify_base_files(plan, tmp_path)
