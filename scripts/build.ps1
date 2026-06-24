# Build burnish and install it to %USERPROFILE%\bin, so `burnish` is current on
# PATH for manual testing after every build. Wired to Ctrl-Shift-B by
# .vscode/tasks.json. Builds straight to the destination (go build -o does build
# and place in one atomic step), so a failed build leaves the previous binary
# untouched rather than installing a half-written one.
$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$binDir   = Join-Path $env:USERPROFILE 'bin'
$outFile  = Join-Path $binDir 'burnish.exe'

if (-not (Test-Path $binDir)) {
    New-Item -ItemType Directory -Path $binDir | Out-Null
}

Write-Host "Building burnish -> $outFile"
Push-Location $repoRoot
try {
    go build -o $outFile ./cmd/burnish
    $code = $LASTEXITCODE
}
finally {
    Pop-Location
}

if ($code -ne 0) {
    Write-Host "Build FAILED (exit $code); existing binary left in place." -ForegroundColor Red
    exit $code
}
Write-Host "Installed: $outFile" -ForegroundColor Green
