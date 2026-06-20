$ErrorActionPreference = 'Stop'
$root = 'C:\Users\jagan\Projects\project\throttle'
$daemon = Join-Path $root 'bin\throttled.exe'
$addr = '127.0.0.1:7886'

$sandbox = Join-Path $env:TEMP ('throttle-dbgm5-' + (Get-Random))
$gem = Join-Path $sandbox '.gemini'
New-Item -ItemType Directory -Force -Path $gem, (Join-Path $sandbox '.throttle'), (Join-Path $sandbox '.claude\projects'), (Join-Path $sandbox '.codex') | Out-Null
$env:USERPROFILE = $sandbox
$env:HOME = $sandbox
$env:THROTTLE_DIR = Join-Path $sandbox '.throttle'

# pre-populate telemetry BEFORE start so the initial scan must see it
Copy-Item (Join-Path $root 'testdata\gemini_telemetry.log') (Join-Path $gem 'telemetry.log')
Write-Output ("gemini file exists pre-start: " + (Test-Path (Join-Path $gem 'telemetry.log')))

$proc = Start-Process -FilePath $daemon -ArgumentList '--addr',$addr -PassThru -NoNewWindow `
  -RedirectStandardOutput (Join-Path $sandbox o.log) -RedirectStandardError (Join-Path $sandbox e.log)
try {
  for ($i=0;$i -lt 40;$i++){Start-Sleep -Milliseconds 250; try { if((Invoke-RestMethod "http://$addr/api/health").ok){break} } catch {}}
  Start-Sleep -Milliseconds 600
  Write-Output '=== /api/sessions ==='
  Invoke-RestMethod "http://$addr/api/sessions" | ConvertTo-Json -Depth 5
  Write-Output '=== daemon stdout ==='
  Get-Content (Join-Path $sandbox o.log)
  Write-Output '=== daemon stderr ==='
  Get-Content (Join-Path $sandbox e.log)
}
finally {
  if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force }
  Remove-Item -Recurse -Force $sandbox -ErrorAction SilentlyContinue
}
