$ErrorActionPreference = 'Stop'
$files = Get-ChildItem 'C:\Users\jagan\.codex\sessions' -Recurse -Filter *.jsonl
Write-Output ("Rollout files: " + $files.Count)

# 1) source shapes across all session_meta (subagent detection)
Write-Output "`n===== session_meta.source shapes ====="
$srcShapes = @{}
$subagentFiles = @()
foreach ($file in $files) {
  $first = Get-Content $file.FullName -TotalCount 1
  if (-not $first) { continue }
  try { $o = $first | ConvertFrom-Json } catch { continue }
  $src = $o.payload.source
  if ($null -eq $src) { $k = '<null>' }
  elseif ($src -is [string]) { $k = "string:$src" }
  else {
    $k = "object:{" + (($src.PSObject.Properties.Name) -join ',') + "}"
    $subagentFiles += $file.FullName
  }
  if ($srcShapes.ContainsKey($k)) { $srcShapes[$k]++ } else { $srcShapes[$k] = 1 }
}
$srcShapes.GetEnumerator() | Sort-Object Value -Descending | ForEach-Object { Write-Output ("  {0}  x{1}" -f $_.Key, $_.Value) }
if ($subagentFiles.Count -gt 0) {
  Write-Output "`nFIRST subagent session_meta payload:"
  $sf = Get-Content $subagentFiles[0] -TotalCount 1 | ConvertFrom-Json
  Write-Output ($sf.payload | ConvertTo-Json -Compress -Depth 8)
}

# 2) Find a real token_count with non-null info
Write-Output "`n===== first token_count with info ====="
$found = $false
foreach ($file in ($files | Sort-Object Length -Descending | Select-Object -First 6)) {
  foreach ($ln in (Get-Content $file.FullName)) {
    if ($ln -notlike '*token_count*') { continue }
    try { $o = $ln | ConvertFrom-Json } catch { continue }
    if ($o.payload.type -eq 'token_count' -and $o.payload.info) {
      Write-Output ("FROM: " + $file.Name)
      Write-Output ("info: " + ($o.payload.info | ConvertTo-Json -Compress -Depth 8))
      $found = $true; break
    }
  }
  if ($found) { break }
}
if (-not $found) { Write-Output "NONE FOUND with info" }
