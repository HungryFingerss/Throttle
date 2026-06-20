$ErrorActionPreference = 'Stop'
$root = 'C:\Users\jagan\Projects\project\throttle'
Set-Location $root

$dirs = @(
  'cmd\throttled',
  'cmd\throttle-hook',
  'internal\watch',
  'internal\adapters\claude',
  'internal\adapters\codex',
  'internal\adapters\gemini',
  'internal\adapters\aider',
  'internal\tally',
  'internal\prices',
  'internal\store',
  'internal\api',
  'internal\enforce',
  'internal\config',
  'web',
  'installer',
  'testdata',
  'testdata\real-captures'
)
foreach ($d in $dirs) {
  New-Item -ItemType Directory -Path (Join-Path $root $d) -Force | Out-Null
}
Write-Output 'Directories created.'
Get-ChildItem $root -Directory | Select-Object -ExpandProperty Name
