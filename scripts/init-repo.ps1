$ErrorActionPreference = 'Stop'
$go = 'C:\Users\jagan\go-sdk\go\bin\go.exe'
$root = 'C:\Users\jagan\Projects\project\throttle'
Set-Location $root

if (-not (Test-Path (Join-Path $root '.git'))) {
  git init -b main | Out-Null
  Write-Output 'git initialized (main)'
} else { Write-Output 'git already initialized' }

git config user.name  | Out-Null
if (-not (git config user.name)) { git config user.name 'Throttle Dev Agent' }
if (-not (git config user.email)) { git config user.email 'dev@throttle.local' }

if (-not (Test-Path (Join-Path $root 'go.mod'))) {
  & $go mod init github.com/jagannivas/throttle
  Write-Output 'go module initialized'
} else { Write-Output 'go.mod already exists' }

& $go version
Write-Output '--- go.mod ---'
Get-Content (Join-Path $root 'go.mod')
