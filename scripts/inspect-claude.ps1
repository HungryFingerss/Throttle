$ErrorActionPreference = 'Stop'
$f = 'C:\Users\jagan\.claude\projects\C--Users-jagan-dog-ai\781515e8-1fb2-418e-9ed0-97bdef24ffa2.jsonl'
$lines = Get-Content $f
Write-Output ("TOTAL LINES: " + $lines.Count)

Write-Output "`n===== LINE 1 top-level keys ====="
$l1 = $lines[0] | ConvertFrom-Json
$l1.PSObject.Properties.Name -join ', '
Write-Output ("type = " + $l1.type)

Write-Output "`n===== distinct top-level 'type' values ====="
$lines | ForEach-Object { try { ($_ | ConvertFrom-Json).type } catch {} } | Group-Object | Select-Object Count, Name | Sort-Object Count -Descending

Write-Output "`n===== first assistant line w/ usage ====="
foreach ($ln in $lines) {
  try { $o = $ln | ConvertFrom-Json } catch { continue }
  if ($o.type -eq 'assistant' -and $o.message -and $o.message.usage) {
    Write-Output ("top-level keys: " + ($o.PSObject.Properties.Name -join ', '))
    Write-Output ("requestId: " + $o.requestId)
    Write-Output ("timestamp: " + $o.timestamp)
    Write-Output ("isSidechain: " + $o.isSidechain)
    Write-Output ("sessionId: " + $o.sessionId)
    Write-Output ("cwd: " + $o.cwd)
    Write-Output ("message.model: " + $o.message.model)
    Write-Output ("message keys: " + ($o.message.PSObject.Properties.Name -join ', '))
    Write-Output ("usage: " + ($o.message.usage | ConvertTo-Json -Compress))
    break
  }
}
