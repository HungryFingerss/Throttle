$ErrorActionPreference = 'Stop'
$root = 'C:\Users\jagan\Projects\project\throttle'
$daemon = Join-Path $root 'bin\throttled.exe'
$hook = Join-Path $root 'bin\throttle-hook.exe'
$addr = '127.0.0.1:7881'

$sandbox = Join-Path $env:TEMP ('throttle-dbg-' + (Get-Random))
$claudeCfg = Join-Path $sandbox '.claude'
$encDir = Join-Path $claudeCfg 'projects\C--proj-m2'
$throttleDir = Join-Path $sandbox '.throttle'
New-Item -ItemType Directory -Force -Path $encDir, $throttleDir | Out-Null
$env:CLAUDE_CONFIG_DIR = $claudeCfg
$env:THROTTLE_DIR = $throttleDir

$proc = Start-Process -FilePath $daemon -ArgumentList '--addr',$addr -PassThru -NoNewWindow `
  -RedirectStandardOutput (Join-Path $sandbox o.log) -RedirectStandardError (Join-Path $sandbox e.log)
try {
  for ($i=0;$i -lt 40;$i++){Start-Sleep -Milliseconds 250; try { if((Invoke-RestMethod "http://$addr/api/health").ok){break} } catch {}}

  Set-Content -Path (Join-Path $encDir 'cap-1.jsonl') -Encoding ascii -Value '{"type":"assistant","cwd":"C:\\proj\\m2","sessionId":"cap-1","requestId":"r1","message":{"model":"claude-sonnet-4-6","id":"r1","usage":{"input_tokens":1000,"output_tokens":1000}}}'
  Start-Sleep -Milliseconds 700

  Write-Output '=== /api/sessions ==='
  Invoke-RestMethod "http://$addr/api/sessions" | ConvertTo-Json -Depth 6

  Write-Output '=== POST /api/caps ==='
  Invoke-RestMethod "http://$addr/api/caps" -Method Post -ContentType 'application/json' -Body '{"scope":"global","caps":{"session_usd":0.01}}' | ConvertTo-Json -Depth 6

  Write-Output '=== POST /v1/check (direct) ==='
  $chk = Invoke-RestMethod "http://$addr/v1/check" -Method Post -ContentType 'application/json' -Body '{"tool":"claude","session_id":"cap-1","event":"PreToolUse"}'
  $chk | ConvertTo-Json -Depth 6

  Write-Output '=== hook binary ==='
  $payload = '{"session_id":"cap-1","transcript_path":"x","hook_event_name":"PreToolUse","tool_name":"Bash"}'
  $out = $payload | & $hook --tool claude --addr $addr
  Write-Output "hook stdout: [$out] exit: $LASTEXITCODE"
}
finally {
  if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force }
  Remove-Item -Recurse -Force $sandbox -ErrorAction SilentlyContinue
}
