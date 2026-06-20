$ErrorActionPreference = 'Stop'
$root = 'C:\Users\jagan\Projects\project\throttle'
$daemon = Join-Path $root 'bin\throttled.exe'
$addr = '127.0.0.1:7884'

$sandbox = Join-Path $env:TEMP ('throttle-m4-' + (Get-Random))
$codexHome = Join-Path $sandbox '.codex'
$sessRoot = Join-Path $codexHome 'sessions\2026\06\20'
New-Item -ItemType Directory -Force -Path $sessRoot, (Join-Path $sandbox '.throttle'), (Join-Path $sandbox '.claude\projects') | Out-Null

# subscription auth
Set-Content (Join-Path $codexHome 'auth.json') -Encoding ascii -Value '{"auth_mode":"chatgpt","tokens":{"refresh_token":"x"}}'

$env:CODEX_HOME = $codexHome
$env:CLAUDE_CONFIG_DIR = Join-Path $sandbox '.claude'   # sandbox claude too (avoid real logs)
$env:THROTTLE_DIR = Join-Path $sandbox '.throttle'

$proc = Start-Process -FilePath $daemon -ArgumentList '--addr',$addr -PassThru -NoNewWindow `
  -RedirectStandardOutput (Join-Path $sandbox o.log) -RedirectStandardError (Join-Path $sandbox e.log)
try {
  for ($i=0;$i -lt 40;$i++){Start-Sleep -Milliseconds 250; try { if((Invoke-RestMethod "http://$addr/api/health").ok){break} } catch {}}

  Copy-Item (Join-Path $root 'testdata\codex_session.jsonl') `
    (Join-Path $sessRoot 'rollout-2026-06-20T10-00-00-00000000-0000-0000-0000-000000000001.jsonl')
  Start-Sleep -Milliseconds 700

  $s = Invoke-RestMethod "http://$addr/api/sessions" -TimeoutSec 3
  $s = @($s)
  Write-Output ("DISCOVERED: tool=" + $s[0].tool + " mode=" + $s[0].mode + " model=" + $s[0].model + " cost=" + $s[0].cost_usd + " tokens=" + ($s[0].tokens.in + $s[0].tokens.out + $s[0].tokens.cache_read))
  if ($s.Count -ne 1) { throw "want 1 session, got $($s.Count)" }
  if ($s[0].tool -ne 'codex') { throw "tool wrong" }
  if ($s[0].mode -ne 'subscription') { throw "mode wrong: $($s[0].mode)" }
  if ($s[0].model -ne 'gpt-5-mini') { throw "model wrong: $($s[0].model)" }
  if ([math]::Abs($s[0].cost_usd - 0.00322125) -gt 1e-9) { throw "cost wrong: $($s[0].cost_usd)" }

  # drop subagent rollout -> must NOT create a row, must NOT add tokens
  Copy-Item (Join-Path $root 'testdata\codex_subagent.jsonl') `
    (Join-Path $sessRoot 'rollout-2026-06-20T11-00-00-00000000-0000-0000-0000-000000000002.jsonl')
  Start-Sleep -Milliseconds 700
  $s2 = @(Invoke-RestMethod "http://$addr/api/sessions" -TimeoutSec 3)
  Write-Output ("AFTER SUBAGENT: rows=" + $s2.Count + " total_tokens=" + ($s2[0].tokens.in + $s2[0].tokens.out + $s2[0].tokens.cache_read))
  if ($s2.Count -ne 1) { throw "subagent leaked a row: $($s2.Count) rows" }
  if (($s2[0].tokens.in + $s2[0].tokens.out + $s2[0].tokens.cache_read) -ne 1660) { throw "subagent tokens leaked" }

  Write-Output 'SMOKE M4: PASS'
}
finally {
  if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force }
  Remove-Item -Recurse -Force $sandbox -ErrorAction SilentlyContinue
}
