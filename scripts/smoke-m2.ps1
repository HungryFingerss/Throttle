$ErrorActionPreference = 'Stop'
$root = 'C:\Users\jagan\Projects\project\throttle'
$daemon = Join-Path $root 'bin\throttled.exe'
$hook = Join-Path $root 'bin\throttle-hook.exe'
$addr = '127.0.0.1:7879'

$sandbox = Join-Path $env:TEMP ('throttle-m2-' + (Get-Random))
$claudeCfg = Join-Path $sandbox '.claude'
$encDir = Join-Path $claudeCfg 'projects\C--proj-m2'
$throttleDir = Join-Path $sandbox '.throttle'
New-Item -ItemType Directory -Force -Path $encDir, $throttleDir | Out-Null

$env:CLAUDE_CONFIG_DIR = $claudeCfg
$env:THROTTLE_DIR = $throttleDir
$env:THROTTLE_ADDR = $addr

$outLog = Join-Path $sandbox 'd.out.log'
$errLog = Join-Path $sandbox 'd.err.log'

# PreToolUse payload the way Claude sends it on stdin.
# NOTE: feed via a stdin file redirect through cmd.exe — PowerShell's string
# pipe to a native exe does NOT deliver stdin reliably. Claude Code feeds hooks
# via a real stdin pipe, which behaves like this redirect.
$payload = '{"session_id":"cap-1","transcript_path":"x","hook_event_name":"PreToolUse","tool_name":"Bash","cwd":"C:\\proj\\m2"}'
$payloadFile = Join-Path $sandbox 'payload.json'
Set-Content -Path $payloadFile -Encoding ascii -Value $payload

function Run-Hook {
  $o = cmd /c "`"$hook`" --tool claude --addr $addr < `"$payloadFile`""
  return (($o -join "`n").Trim())
}

$proc = Start-Process -FilePath $daemon -ArgumentList '--addr',$addr -PassThru -NoNewWindow `
  -RedirectStandardOutput $outLog -RedirectStandardError $errLog
try {
  $ok = $false
  for ($i=0; $i -lt 40; $i++) { Start-Sleep -Milliseconds 250; try { if ((Invoke-RestMethod "http://$addr/api/health" -TimeoutSec 2).ok) { $ok=$true; break } } catch {} }
  if (-not $ok) { throw 'daemon not healthy' }

  # session worth $0.018
  $sess = Join-Path $encDir 'cap-1.jsonl'
  Set-Content -Path $sess -Encoding ascii -Value '{"type":"assistant","cwd":"C:\\proj\\m2","sessionId":"cap-1","requestId":"r1","message":{"model":"claude-sonnet-4-6","id":"r1","usage":{"input_tokens":1000,"output_tokens":1000}}}'
  Start-Sleep -Milliseconds 600

  # cap below spend -> hook must DENY
  Invoke-RestMethod "http://$addr/api/caps" -Method Post -ContentType 'application/json' `
    -Body '{"scope":"global","caps":{"session_usd":0.01}}' | Out-Null
  $deny = Run-Hook
  Write-Output "OVER-CAP hook stdout: $deny"
  if ($deny -notmatch 'deny') { throw "expected deny, got: $deny" }

  # raise cap -> hook must ALLOW (silent)
  Invoke-RestMethod "http://$addr/api/caps" -Method Post -ContentType 'application/json' `
    -Body '{"scope":"global","caps":{"session_usd":100}}' | Out-Null
  $allow = Run-Hook
  Write-Output "UNDER-CAP hook stdout: '$allow'"
  if ($allow.Trim() -ne '') { throw "expected silent allow, got: $allow" }

  Write-Output 'CAP ENFORCEMENT: PASS'
}
finally {
  if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force }
}

# Daemon now DOWN -> hook must FAIL-OPEN (silent + exit 0)
Start-Sleep -Milliseconds 400
$failopen = cmd /c "`"$hook`" --tool claude --addr $addr < `"$payloadFile`""
$code = $LASTEXITCODE
Write-Output "DAEMON-DOWN hook stdout: '$failopen' exit: $code"
if ($code -ne 0) { throw "fail-open must exit 0, got $code" }
if (($failopen -join '').Trim() -ne '') { throw "fail-open must be silent, got: $failopen" }
Write-Output 'FAIL-OPEN: PASS'

Remove-Item -Recurse -Force $sandbox -ErrorAction SilentlyContinue
Write-Output 'SMOKE M2: PASS'
