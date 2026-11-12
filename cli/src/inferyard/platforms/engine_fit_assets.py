"""The installed controlled MLX entrypoint, resolved from package data."""

import hashlib
from importlib.resources import files
from pathlib import Path


def mlx_server_path():
    """Return the installed script, never a source-checkout-relative path."""
    return Path(str(files("inferyard").joinpath("data/engine_fit/mlx_server.py")))


def mlx_server_sha256():
    return hashlib.sha256(mlx_server_path().read_bytes()).hexdigest()
