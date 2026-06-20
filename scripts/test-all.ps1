$ErrorActionPreference = 'Continue'
$root = 'C:\Users\jagan\Projects\project\throttle'
Set-Location $root
$results = [ordered]@{}

function Step($name, $block) {
  Write-Output ("`n==================== " + $name + " ====================")
  try { & $block; $results[$name] = ($LASTEXITCODE -eq 0 -or $null -eq $LASTEXITCODE) }
  catch { Write-Output ("FAILED: " + $_.Exception.Message); $results[$name] = $false }
}

# --- build current-OS binaries + the win32-x64 dist used by the installer smoke
Step 'build binaries' {
  & "$root\scripts\go.ps1" build -o bin/throttled.exe ./cmd/throttled
  & "$root\scripts\go.ps1" build -o bin/throttle-hook.exe ./cmd/throttle-hook
  New-Item -ItemType Directory -Force -Path "$root\installer\dist\win32-x64" | Out-Null
  Copy-Item "$root\bin\throttled.exe","$root\bin\throttle-hook.exe" "$root\installer\dist\win32-x64\" -Force
}

Step 'go test ./...' { & "$root\scripts\go.ps1" test ./... }

Step 'node installer tests' {
  Push-Location "$root\installer"; node --test; $script:nodeExit = $LASTEXITCODE; Pop-Location
  if ($script:nodeExit -ne 0) { $global:LASTEXITCODE = $script:nodeExit }
}

# --- sandboxed smokes, each in a fresh shell so env never leaks between them
$psexe = [System.Diagnostics.Process]::GetCurrentProcess().MainModule.FileName
foreach ($smoke in 'smoke-m1','smoke-m2','smoke-m3','smoke-m4','smoke-m5','smoke-m6') {
  Step $smoke {
    & $psexe -NoProfile -ExecutionPolicy Bypass -File "$root\scripts\$smoke.ps1"
  }
}

Write-Output "`n==================== SUMMARY ===================="
$allPass = $true
foreach ($k in $results.Keys) {
  $ok = $results[$k]
  if (-not $ok) { $allPass = $false }
  Write-Output ("{0,-22} {1}" -f $k, ($(if ($ok) { 'PASS' } else { 'FAIL' })))
}
if ($allPass) { Write-Output "`nALL GREEN"; exit 0 } else { Write-Output "`nSOME FAILURES"; exit 1 }
