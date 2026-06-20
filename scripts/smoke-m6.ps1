$ErrorActionPreference = 'Stop'
$root = 'C:\Users\jagan\Projects\project\throttle'
$throttle = Join-Path $root 'installer\bin\throttle.js'
$binSrc = Join-Path $root 'installer\dist\win32-x64'   # validate the cross-built binaries too
$addr = '127.0.0.1:7887'

$sandbox = Join-Path $env:TEMP ('throttle-m6-' + (Get-Random))
$claude = Join-Path $sandbox '.claude'
New-Item -ItemType Directory -Force -Path $claude, (Join-Path $sandbox '.codex') | Out-Null

# Sandbox EVERYTHING: installer + the daemon it spawns inherit these.
$env:USERPROFILE = $sandbox
$env:HOME = $sandbox
$env:THROTTLE_DIR = Join-Path $sandbox '.throttle'
$env:THROTTLE_NO_PRICE_REFRESH = '1'
Remove-Item Env:CLAUDE_CONFIG_DIR, Env:CODEX_HOME, Env:THROTTLE_FAKE_HOME -ErrorAction SilentlyContinue

$settings = Join-Path $claude 'settings.json'

function HasThrottleHook { (Test-Path $settings) -and ((Get-Content $settings -Raw) -match 'throttle-hook') }

try {
  Write-Output '=== init ==='
  & node $throttle init --no-open --bin-src $binSrc --addr $addr
  if ($LASTEXITCODE -ne 0) { throw "init exit $LASTEXITCODE" }

  # daemon healthy
  $ok = $false
  for ($i=0; $i -lt 40; $i++) { Start-Sleep -Milliseconds 250; try { if ((Invoke-RestMethod "http://$addr/api/health").ok) { $ok=$true; break } } catch {} }
  if (-not $ok) { throw 'daemon not healthy after init' }
  Write-Output 'HEALTH: ok'

  if (-not (HasThrottleHook)) { throw 'claude hooks not wired' }
  Write-Output 'HOOKS: wired'

  # status reports running
  & node $throttle status
  Write-Output '=== uninstall ==='
  & node $throttle uninstall
  if ($LASTEXITCODE -ne 0) { throw "uninstall exit $LASTEXITCODE" }

  Start-Sleep -Milliseconds 500
  $down = $false
  try { Invoke-RestMethod "http://$addr/api/health" -TimeoutSec 2 | Out-Null } catch { $down = $true }
  if (-not $down) { throw 'daemon still running after uninstall' }
  Write-Output 'DAEMON: stopped'

  if (HasThrottleHook) { throw 'hooks not removed by uninstall' }
  Write-Output 'HOOKS: removed'

  Write-Output 'SMOKE M6: PASS'
}
finally {
  # belt-and-suspenders: make sure no daemon survives the test
  try { & node $throttle stop | Out-Null } catch {}
  Get-Process throttled -ErrorAction SilentlyContinue | Where-Object { $_.Path -like "$sandbox*" } | Stop-Process -Force -ErrorAction SilentlyContinue
  Remove-Item -Recurse -Force $sandbox -ErrorAction SilentlyContinue
}
