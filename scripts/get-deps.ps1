$ErrorActionPreference = 'Stop'
$go = 'C:\Users\jagan\go-sdk\go\bin\go.exe'
$env:Path = 'C:\Users\jagan\go-sdk\go\bin;' + $env:Path
$env:GOTOOLCHAIN = 'local'
Set-Location 'C:\Users\jagan\Projects\project\throttle'

Write-Output 'Fetching fsnotify...'
& $go get github.com/fsnotify/fsnotify@latest
Write-Output 'Fetching gorilla/websocket...'
& $go get github.com/gorilla/websocket@latest

Write-Output '--- go.mod after get ---'
Get-Content go.mod
