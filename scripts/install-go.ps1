$ErrorActionPreference = 'Stop'
$url = 'https://go.dev/dl/go1.26.4.windows-amd64.zip'
$zip = Join-Path $env:TEMP 'go1.26.4.zip'
$dest = 'C:\Users\jagan\go-sdk'

Write-Output 'Downloading Go 1.26.4...'
(New-Object System.Net.WebClient).DownloadFile($url, $zip)
Write-Output ('Downloaded {0:N1} MB' -f ((Get-Item $zip).Length / 1MB))

if (Test-Path $dest) { Remove-Item $dest -Recurse -Force }
New-Item -ItemType Directory -Path $dest -Force | Out-Null

Write-Output 'Extracting...'
Expand-Archive -Path $zip -DestinationPath $dest -Force
Write-Output ('go.exe present: ' + (Test-Path (Join-Path $dest 'go\bin\go.exe')))
