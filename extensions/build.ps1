# Build all extension binaries on Windows.
# Run from project root:  powershell -File extensions/build.ps1
param()

$extDir = Join-Path $PSScriptRoot ""
$projectRoot = Split-Path $extDir -Parent
Set-Location $projectRoot

Write-Host "=== Building extensions (Windows) ==="

$built = 0
$skipped = 0
$failed = 0

Get-ChildItem $extDir -Directory | ForEach-Object {
    $dir = $_.FullName
    $name = $_.Name
    $goMod = Join-Path $dir "go.mod"

    if (-not (Test-Path $goMod)) {
        Write-Host "  SKIP $name (no go.mod)"
        $script:skipped++
        return
    }

    # Read manifest.json for binary name
    $outputName = $name
    $mfPath = Join-Path $dir "manifest.json"
    if (Test-Path $mfPath) {
        try {
            $mf = Get-Content $mfPath -Raw | ConvertFrom-Json
            if ($mf.binary) { $outputName = $mf.binary }
        } catch {}
    }

    Write-Host "  BUILD $name"
    $out = Join-Path $dir "$outputName.exe"

    Push-Location $dir
    try {
        $result = & go build -ldflags "-s -w" -o $out . 2>&1
        if ($LASTEXITCODE -eq 0) {
            Write-Host "    OK: $out"
            $script:built++
        } else {
            Write-Host "    FAIL: $result"
            $script:failed++
        }
    } catch {
        Write-Host "    FAIL: $_"
        $script:failed++
    }
    Pop-Location
}

Write-Host ""
Write-Host "=== Done: $built built, $skipped skipped, $failed failed ==="

if ($built -gt 0) {
    $dest = Join-Path $env:USERPROFILE ".openhuman\extensions"
    Write-Host ""
    Write-Host "To install, copy extension directories to:"
    Write-Host "  $dest"
}
