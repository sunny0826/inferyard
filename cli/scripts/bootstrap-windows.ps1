. (Join-Path $PSScriptRoot 'windows-env.ps1')
$benchUvVersion = '0.12.18'
$benchPythonVersion = '3.14.7'
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') {
    throw 'This bootstrap currently supports Windows x64.'
}
if (-not (Test-Path -LiteralPath $benchUv)) {
    $benchArchive = Join-Path $benchTools 'uv.zip'
    Invoke-WebRequest -UseBasicParsing -Uri "https://github.com/astral-sh/uv/releases/download/$benchUvVersion/uv-x86_64-pc-windows-msvc.zip" -OutFile $benchArchive
    Expand-Archive -LiteralPath $benchArchive -DestinationPath (Join-Path $benchTools 'uv') -Force
}
if ((& $benchUv --version) -notlike "uv $benchUvVersion *") {
    throw "Expected uv $benchUvVersion. Inspect .tools/uv before replacing an existing tool."
}
Push-Location $benchRoot
try {
    & $benchUv python install $benchPythonVersion --no-bin --no-registry
    if ($LASTEXITCODE -ne 0) { throw 'Python installation failed.' }
    & $benchUv sync --frozen --python $benchPythonVersion
    if ($LASTEXITCODE -ne 0) { throw 'Dependency synchronization failed.' }
    & $benchUv run --frozen python --version
    if ($LASTEXITCODE -ne 0) { throw 'Python verification failed.' }
} finally {
    Pop-Location
}
