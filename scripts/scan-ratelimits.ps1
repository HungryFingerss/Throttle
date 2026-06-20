$ErrorActionPreference = 'Stop'
$files = Get-ChildItem 'C:\Users\jagan\.codex\sessions' -Recurse -Filter *.jsonl | Sort-Object Length -Descending
foreach ($file in $files) {
  foreach ($ln in (Get-Content $file.FullName)) {
    if ($ln -notlike '*rate_limits*') { continue }
    try { $o = $ln | ConvertFrom-Json } catch { continue }
    $rl = $o.payload.rate_limits
    if ($rl -and ($rl.primary -or $rl.secondary)) {
      Write-Output ("FROM: " + $file.Name)
      Write-Output ($rl | ConvertTo-Json -Compress -Depth 8)
      exit 0
    }
  }
}
Write-Output 'No populated rate_limits.primary found in real logs (all null on this machine).'
