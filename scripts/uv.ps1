# Forward uv arguments without requiring PATH changes or environment activation.
. (Join-Path $PSScriptRoot 'windows-env.ps1')
if (-not (Test-Path -LiteralPath $benchUv)) {
    throw 'Run scripts/bootstrap-windows.ps1 first.'
}
Push-Location $benchRoot
try {
    if ($MyInvocation.ExpectingInput) {
        $input | & $benchUv @args
    } else {
        & $benchUv @args
    }
    $benchExit = $LASTEXITCODE
} finally {
    Pop-Location
}
exit $benchExit
