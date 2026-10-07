# Dot-source this file to keep every development write on D:.
$ErrorActionPreference = 'Stop'
$benchRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if ([System.IO.Path]::GetPathRoot($benchRoot) -ne 'D:\') {
    throw 'The Windows development workspace must be on D:.'
}
$benchTools = Join-Path $benchRoot '.tools'
$benchPaths = @{
    UV_CACHE_DIR = (Join-Path $benchRoot '.uv-cache')
    UV_PROJECT_ENVIRONMENT = (Join-Path $benchRoot '.venv')
    UV_PYTHON_INSTALL_DIR = (Join-Path $benchTools 'python')
    UV_PYTHON_CACHE_DIR = (Join-Path $benchTools 'python-cache')
    UV_PYTHON_BIN_DIR = (Join-Path $benchTools 'bin')
    UV_TOOL_DIR = (Join-Path $benchTools 'uv-tools')
    UV_TOOL_BIN_DIR = (Join-Path $benchTools 'bin')
    UV_CREDENTIALS_DIR = (Join-Path $benchTools 'credentials')
    PIP_CACHE_DIR = (Join-Path $benchTools 'pip-cache')
    HF_HOME = (Join-Path $benchTools 'huggingface')
    HF_HUB_CACHE = (Join-Path $benchTools 'huggingface\hub')
    HF_ASSETS_CACHE = (Join-Path $benchTools 'huggingface\assets')
    XDG_CACHE_HOME = (Join-Path $benchTools 'cache')
    LLAMA_CACHE = (Join-Path $benchTools 'llama-cache')
    TEMP = (Join-Path $benchTools 'tmp')
    TMP = (Join-Path $benchTools 'tmp')
}
foreach ($benchKey in $benchPaths.Keys) {
    New-Item -ItemType Directory -Force -Path $benchPaths[$benchKey] | Out-Null
    [Environment]::SetEnvironmentVariable($benchKey, $benchPaths[$benchKey], 'Process')
}
$env:UV_PYTHON_INSTALL_REGISTRY = '0'
$env:UV_PYTHON_INSTALL_BIN = '0'
$env:UV_PYTHON_NO_REGISTRY = '1'
$env:UV_NO_MODIFY_PATH = '1'
$env:PYTHONUTF8 = '1'
$env:PYTHONIOENCODING = 'utf-8'
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$benchUv = Join-Path $benchTools 'uv\uv.exe'
