$ErrorActionPreference = 'Stop'
$root = 'C:\Users\jagan\Projects\project\throttle'
$daemon = Join-Path $root 'bin\throttled.exe'
$hook = Join-Path $root 'bin\throttle-hook.exe'
$addr = '127.0.0.1:7883'

$sandbox = Join-Path $env:TEMP ('throttle-m3-' + (Get-Random))
$claudeCfg = Join-Path $sandbox '.claude'
New-Item -ItemType Directory -Force -Path (Join-Path $claudeCfg 'projects'), (Join-Path $sandbox '.throttle') | Out-Null
$env:CLAUDE_CONFIG_DIR = $claudeCfg
$env:THROTTLE_DIR = Join-Path $sandbox '.throttle'

# hook payload files
$promptPayload = Join-Path $sandbox 'prompt.json'
Set-Content $promptPayload -Encoding ascii -Value '{"session_id":"r-1","transcript_path":"x","hook_event_name":"UserPromptSubmit"}'
$compactPayload = Join-Path $sandbox 'compact.json'
Set-Content $compactPayload -Encoding ascii -Value '{"session_id":"r-1","transcript_path":"x","hook_event_name":"SessionStart","source":"compact"}'

function Hook($file) { return ((cmd /c "`"$hook`" --tool claude --addr $addr < `"$file`"") -join "`n") }

$proc = Start-Process -FilePath $daemon -ArgumentList '--addr',$addr -PassThru -NoNewWindow `
  -RedirectStandardOutput (Join-Path $sandbox o.log) -RedirectStandardError (Join-Path $sandbox e.log)
try {
  for ($i=0;$i -lt 40;$i++){Start-Sleep -Milliseconds 250; try { if((Invoke-RestMethod "http://$addr/api/health").ok){break} } catch {}}

  Invoke-RestMethod "http://$addr/api/rules" -Method Post -ContentType 'application/json' `
    -Body '{"scope":"global","rules":["Never force-push to main","Run tests before saying done"]}' | Out-Null

  $p = Hook $promptPayload
  Write-Output "PROMPT inject: $p"
  if ($p -notmatch 'additionalContext' -or $p -notmatch 'force-push') { throw "rules not injected on prompt" }

  $c = Hook $compactPayload
  Write-Output "COMPACT inject: $c"
  if ($c -notmatch 'force-push') { throw "rules did NOT survive compaction" }
  Write-Output 'RULES + COMPACTION: PASS'

  # one-off message delivered once
  Invoke-RestMethod "http://$addr/api/message" -Method Post -ContentType 'application/json' `
    -Body '{"session_id":"r-1","message":"switch to the staging DB"}' | Out-Null
  $m1 = Hook $promptPayload
  if ($m1 -notmatch 'staging DB') { throw "one-off not delivered" }
  $m2 = Hook $promptPayload
  if ($m2 -match 'staging DB') { throw "one-off delivered twice" }
  Write-Output 'ONE-OFF MESSAGE: PASS'

  Write-Output 'SMOKE M3: PASS'
}
finally {
  if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force }
  Remove-Item -Recurse -Force $sandbox -ErrorAction SilentlyContinue
}
