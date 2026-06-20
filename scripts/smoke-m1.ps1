$ErrorActionPreference = 'Stop'
$root = 'C:\Users\jagan\Projects\project\throttle'
$exe = Join-Path $root 'bin\throttled.exe'

# --- sandbox dirs (NEVER touch real ~/.claude or ~/.throttle) ---
$sandbox = Join-Path $env:TEMP ('throttle-smoke-' + (Get-Random))
$claudeCfg = Join-Path $sandbox '.claude'
$projects = Join-Path $claudeCfg 'projects'
$encDir = Join-Path $projects 'C--proj-smoke'
$throttleDir = Join-Path $sandbox '.throttle'
New-Item -ItemType Directory -Force -Path $encDir, $throttleDir | Out-Null

$env:CLAUDE_CONFIG_DIR = $claudeCfg
$env:THROTTLE_DIR = $throttleDir
$env:THROTTLE_NO_PRICE_REFRESH = '1'   # deterministic fallback pricing for assertions

$outLog = Join-Path $sandbox 'daemon.out.log'
$errLog = Join-Path $sandbox 'daemon.err.log'

Write-Output "Sandbox: $sandbox"
$proc = Start-Process -FilePath $exe -ArgumentList '--addr','127.0.0.1:7879' `
  -PassThru -NoNewWindow -RedirectStandardOutput $outLog -RedirectStandardError $errLog

try {
  # wait for health
  $ok = $false
  for ($i = 0; $i -lt 40; $i++) {
    Start-Sleep -Milliseconds 250
    try {
      $h = Invoke-RestMethod -Uri 'http://127.0.0.1:7879/api/health' -TimeoutSec 2
      if ($h.ok) { $ok = $true; break }
    } catch {}
  }
  if (-not $ok) { throw 'daemon did not become healthy' }
  Write-Output 'HEALTH: ok'

  # write a session file (post-start; must be discovered via fsnotify)
  $sess = Join-Path $encDir 'smoke-1.jsonl'
  $l1 = '{"type":"assistant","cwd":"C:\\proj\\smoke","sessionId":"smoke-1","requestId":"r1","message":{"model":"claude-sonnet-4-6","id":"r1","usage":{"input_tokens":1000,"output_tokens":1000}}}'
  Set-Content -Path $sess -Value $l1 -Encoding ascii

  # Poll for discovery (fsnotify is near-instant, but be robust under load).
  $s1 = $null
  for ($i = 0; $i -lt 40; $i++) {
    Start-Sleep -Milliseconds 250
    try { $r = Invoke-RestMethod -Uri 'http://127.0.0.1:7879/api/sessions' -TimeoutSec 3 } catch { continue }
    if ($r) { $s1 = $r; break }
  }
  if (-not $s1) { throw 'no session discovered' }
  Write-Output ("DISCOVERED: id=" + $s1[0].id + " path=" + $s1[0].project_path + " cost=" + $s1[0].cost_usd)
  if ([math]::Abs($s1[0].cost_usd - 0.018) -gt 1e-9) { throw "cost wrong: $($s1[0].cost_usd)" }

  # append more usage -> live update; poll until the cost reflects it
  Add-Content -Path $sess -Value '{"type":"assistant","cwd":"C:\\proj\\smoke","sessionId":"smoke-1","requestId":"r2","message":{"model":"claude-opus-4-8","id":"r2","usage":{"input_tokens":1000,"output_tokens":1000}}}' -Encoding ascii
  $s2 = $null
  for ($i = 0; $i -lt 40; $i++) {
    Start-Sleep -Milliseconds 250
    try { $r = Invoke-RestMethod -Uri 'http://127.0.0.1:7879/api/sessions' -TimeoutSec 3 } catch { continue }
    if ($r -and [math]::Abs($r[0].cost_usd - 0.108) -lt 1e-9) { $s2 = $r; break }
  }
  if (-not $s2) { throw "cost after append did not reach 0.108" }
  Write-Output ("AFTER APPEND: model=" + $s2[0].model + " cost=" + $s2[0].cost_usd)
  if ($s2[0].model -ne 'claude-opus-4-8') { throw "model attribution wrong: $($s2[0].model)" }

  Write-Output 'SMOKE: PASS'
}
finally {
  if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force }
  Write-Output '--- daemon stdout ---'
  if (Test-Path $outLog) { Get-Content $outLog }
  if (Test-Path $errLog) { Get-Content $errLog }
  Remove-Item -Recurse -Force $sandbox -ErrorAction SilentlyContinue
}
