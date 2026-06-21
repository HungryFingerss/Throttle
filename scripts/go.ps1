$ErrorActionPreference = 'Stop'
# Use a user-local Go SDK if one is present (no system install needed), otherwise
# fall back to `go` on PATH. Keeps `scripts\test-all.ps1` and the smokes working
# whether you installed Go normally or unpacked it locally.
$localGo = Join-Path $env:USERPROFILE 'go-sdk\go\bin'
if (Test-Path $localGo) { $env:Path = "$localGo;" + $env:Path }
$env:GOTOOLCHAIN = 'local'
Set-Location (Join-Path $PSScriptRoot '..')
& go @args
exit $LASTEXITCODE
