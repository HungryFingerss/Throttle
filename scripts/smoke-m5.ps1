$ErrorActionPreference = 'Stop'
$root = 'C:\Users\jagan\Projects\project\throttle'
$daemon = Join-Path $root 'bin\throttled.exe'
$addr = '127.0.0.1:7885'

$sandbox = Join-Path $env:TEMP ('throttle-m5-' + (Get-Random))
$gem = Join-Path $sandbox '.gemini'
$proj = Join-Path $sandbox 'proj'
New-Item -ItemType Directory -Force -Path $gem, $proj, (Join-Path $sandbox '.throttle'), (Join-Path $sandbox '.claude\projects'), (Join-Path $sandbox '.codex') | Out-Null

# Full sandbox: redirect HOME so ~/.gemini etc. resolve under the sandbox.
$env:USERPROFILE = $sandbox
$env:HOME = $sandbox
$env:THROTTLE_DIR = Join-Path $sandbox '.throttle'
$env:THROTTLE_AIDER_DIRS = $proj
$env:THROTTLE_NO_PRICE_REFRESH = '1'

# Pre-populate logs (telemetry.log / history.md normally already exist) — proven
# discovery via the daemon's initial scan; the live-append path is covered by
# the M1/M4 smokes and the gemini truncated-tail unit test.
Copy-Item (Join-Path $root 'testdata\gemini_telemetry.log') (Join-Path $gem 'telemetry.log')
Copy-Item (Join-Path $root 'testdata\aider_history.md') (Join-Path $proj '.aider.chat.history.md')

$proc = Start-Process -FilePath $daemon -ArgumentList '--addr',$addr -PassThru -NoNewWindow `
  -RedirectStandardOutput (Join-Path $sandbox o.log) -RedirectStandardError (Join-Path $sandbox e.log)
try {
  for ($i=0;$i -lt 40;$i++){Start-Sleep -Milliseconds 250; try { if((Invoke-RestMethod "http://$addr/api/health").ok){break} } catch {}}
  Start-Sleep -Milliseconds 500

  # capabilities endpoint lists all four tools
  $c = Invoke-RestMethod "http://$addr/api/capabilities"
  Write-Output ("CAPS tools: claude=" + $c.claude.monitor_confidence + " codex=" + $c.codex.monitor_confidence + " gemini=" + $c.gemini.monitor_confidence + " aider=" + $c.aider.monitor_confidence)
  if (-not $c.claude -or -not $c.codex -or -not $c.gemini -or -not $c.aider) { throw "capabilities missing a tool" }
  if ($c.gemini.hard_cap -ne $false) { throw "gemini should not advertise hard cap" }

  $resp = Invoke-RestMethod "http://$addr/api/sessions"   # do NOT wrap in @() (PS 5.1 double-wraps arrays)
  Write-Output ("SESSIONS: " + (($resp | ForEach-Object { $_.id }) -join ' | '))
  $sessA = $resp | Where-Object { $_.id -eq 'sess-A' } | Select-Object -First 1
  $sessB = $resp | Where-Object { $_.id -eq 'sess-B' } | Select-Object -First 1
  $aiderRow = $resp | Where-Object { $_.tool -eq 'aider' } | Select-Object -First 1
  $placeholder = $resp | Where-Object { $_.id -eq 'gemini-telemetry' } | Select-Object -First 1

  if (-not $sessA -or -not $sessB) { throw "gemini sessions not demuxed" }
  if ($sessA.tool -ne 'gemini') { throw "sess-A wrong tool: $($sessA.tool)" }
  if ([math]::Abs($sessA.cost_usd - 0.0066875) -gt 1e-9) { throw "gemini sess-A cost wrong: $($sessA.cost_usd)" }
  if ($placeholder) { throw "file placeholder leaked as a row" }

  if (-not $aiderRow) { throw "aider session not discovered" }
  if ([math]::Abs($aiderRow.cost_usd - 0.0129) -gt 1e-9) { throw "aider cost wrong: $($aiderRow.cost_usd)" }

  Write-Output 'SMOKE M5: PASS'
}
finally {
  if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force }
  Remove-Item -Recurse -Force $sandbox -ErrorAction SilentlyContinue
}
