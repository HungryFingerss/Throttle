$ErrorActionPreference = 'Stop'
$root = 'C:\Users\jagan\Projects\project\throttle'
$daemon = Join-Path $root 'bin\throttled.exe'
$hook = Join-Path $root 'bin\throttle-hook.exe'
$addr = '127.0.0.1:7882'

$sandbox = Join-Path $env:TEMP ('throttle-hs-' + (Get-Random))
$claudeCfg = Join-Path $sandbox '.claude'
$encDir = Join-Path $claudeCfg 'projects\C--proj-m2'
New-Item -ItemType Directory -Force -Path $encDir, (Join-Path $sandbox '.throttle') | Out-Null
$env:CLAUDE_CONFIG_DIR = $claudeCfg
$env:THROTTLE_DIR = Join-Path $sandbox '.throttle'

$payloadFile = Join-Path $sandbox 'payload.json'
Set-Content -Path $payloadFile -Encoding ascii -Value '{"session_id":"cap-1","transcript_path":"x","hook_event_name":"PreToolUse","tool_name":"Bash"}'

$proc = Start-Process -FilePath $daemon -ArgumentList '--addr',$addr -PassThru -NoNewWindow `
  -RedirectStandardOutput (Join-Path $sandbox o.log) -RedirectStandardError (Join-Path $sandbox e.log)
try {
  for ($i=0;$i -lt 40;$i++){Start-Sleep -Milliseconds 250; try { if((Invoke-RestMethod "http://$addr/api/health").ok){break} } catch {}}
  Set-Content -Path (Join-Path $encDir 'cap-1.jsonl') -Encoding ascii -Value '{"type":"assistant","cwd":"C:\\proj\\m2","sessionId":"cap-1","requestId":"r1","message":{"model":"claude-sonnet-4-6","id":"r1","usage":{"input_tokens":1000,"output_tokens":1000}}}'
  Start-Sleep -Milliseconds 700
  Invoke-RestMethod "http://$addr/api/caps" -Method Post -ContentType 'application/json' -Body '{"scope":"global","caps":{"session_usd":0.01}}' | Out-Null

  Write-Output '--- method A: PS string pipe ---'
  $a = (Get-Content $payloadFile -Raw) | & $hook --tool claude --addr $addr
  Write-Output "A out=[$a]"

  Write-Output '--- method B: cmd stdin redirect ---'
  $b = cmd /c "`"$hook`" --tool claude --addr $addr < `"$payloadFile`""
  Write-Output "B out=[$b]"

  Write-Output '--- method C: ping daemon from a child exe (curl-like via hook of UserPromptSubmit inject path is N/A) ---'
  # Direct connectivity check from a child process context using the daemon health:
  $c = cmd /c "`"$hook`" --tool claude --addr 127.0.0.1:1 < `"$payloadFile`""
  Write-Output "C (bad addr, expect empty/fail-open) out=[$c]"
}
finally {
  if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force }
  Remove-Item -Recurse -Force $sandbox -ErrorAction SilentlyContinue
}
