$ErrorActionPreference = 'Stop'
$f = 'C:\Users\jagan\.codex\sessions\2026\05\13\rollout-2026-05-13T16-11-18-019e20ed-3930-7643-9a72-c5665bfc447d.jsonl'
$lines = Get-Content $f
Write-Output ("TOTAL LINES: " + $lines.Count)

Write-Output "`n===== LINE 1 (session_meta) ====="
$l1 = $lines[0] | ConvertFrom-Json
Write-Output ("top keys: " + ($l1.PSObject.Properties.Name -join ', '))
Write-Output ("type: " + $l1.type)
if ($l1.payload) {
  Write-Output ("payload keys: " + ($l1.payload.PSObject.Properties.Name -join ', '))
  Write-Output ("payload.cwd: " + $l1.payload.cwd)
  Write-Output ("payload.id: " + $l1.payload.id)
  Write-Output ("payload.cli_version: " + $l1.payload.cli_version)
  Write-Output ("payload.source present: " + [bool]$l1.payload.source)
  if ($l1.payload.source) { Write-Output ("source: " + ($l1.payload.source | ConvertTo-Json -Compress -Depth 6)) }
}

Write-Output "`n===== distinct payload.type values ====="
$lines | ForEach-Object { try { ($_ | ConvertFrom-Json).payload.type } catch {} } | Group-Object | Select-Object Count, Name | Sort-Object Count -Descending

Write-Output "`n===== first turn_context ====="
foreach ($ln in $lines) {
  try { $o = $ln | ConvertFrom-Json } catch { continue }
  if ($o.type -eq 'turn_context') {
    Write-Output ("payload: " + ($o.payload | ConvertTo-Json -Compress -Depth 6))
    break
  }
}

Write-Output "`n===== first token_count event_msg ====="
foreach ($ln in $lines) {
  try { $o = $ln | ConvertFrom-Json } catch { continue }
  if ($o.payload -and $o.payload.type -eq 'token_count') {
    Write-Output ("top keys: " + ($o.PSObject.Properties.Name -join ', '))
    Write-Output ("timestamp: " + $o.timestamp)
    Write-Output ("payload: " + ($o.payload | ConvertTo-Json -Compress -Depth 8))
    break
  }
}
